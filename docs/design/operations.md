# Operations

**Status:** Draft, 2026-05-17
**Owners:** TBD

This document covers everything an operator deals with on day 2: install, RBAC, kernel privileges, the **security/threat model**, self-observability, upgrades, debug surfaces, and resource defaults. It satisfies requirement §3 ("Operational requirements") and crosses every component.

## 1. Install

The default install is a single `kubectl apply -f` of a kustomize bundle (also published as a Helm chart). It deploys, in `ollie-system`:

- 2 CRDs (cluster-scoped): `TrafficMonitor`, `ClusterTrafficPolicy`
- 1 ConfigMap: `ollie-config` (operator-tunable defaults)
- 1 ConfigMap: `ollie-sink-catalog` (sink references the webhook validates against)
- 4 ServiceAccounts: `ollie-agent`, `ollie-controller`, `ollie-query`, `ollie-webhook`
- 1 (Cluster)Role per SA + bindings
- 1 DaemonSet: agent
- 2 Deployments: controller (2 replicas), query-server (N replicas, default 2)
- 4 Services: agent (headless, for fan-out), controller (ClusterIP), query (ClusterIP+ HTTPS), webhook (ClusterIP)
- 1 cert-manager Issuer + 2 Certificates (query-server, webhook)
- 1 APIService: `v1beta1.custom.metrics.k8s.io`
- 1 ValidatingWebhookConfiguration
- 1 PodDisruptionBudget per controller and query (`minAvailable: 1`)

Cert-manager is a hard dependency. The install docs call this out and link the upstream install.

## 2. RBAC

Least-privilege per SA. Full YAML in `k8s/rbac/`; below is the privilege summary.

### 2.1 Agent

```yaml
rules:
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch]    # local cache + fallback informer
  - apiGroups: [""]
    resources: [nodes]
    verbs: [get, list]           # self only, via fieldSelector
  - apiGroups: [""]
    resources: [services]
    verbs: [get, list, watch]    # fallback informer only
  - apiGroups: [discovery.k8s.io]
    resources: [endpointslices]
    verbs: [get, list, watch]    # fallback informer only
```

Also reads Kubelet API on `127.0.0.1:10250` via the pod SA's TLS — Kubelet authorizes via `RBAC` against the SA, granted via the agent ClusterRole binding to `nodes/proxy`:

```yaml
  - apiGroups: [""]
    resources: [nodes/proxy]
    verbs: [get]
```

### 2.2 Controller

Per [`control-plane.md`](control-plane.md) §7.

### 2.3 Query server

```yaml
rules: []   # no K8s API access required
```

The query server is the only component that takes external traffic (custom-metrics-API). It does no K8s API calls of its own. RBAC is empty by design.

### 2.4 Webhook

```yaml
rules:
  - apiGroups: [obs.gke-labs.dev]
    resources: [trafficmonitors, clustertrafficpolicies]
    verbs: [get, list]
  - apiGroups: [""]
    resources: [configmaps]
    resourceNames: [ollie-sink-catalog]
    verbs: [get]
```

### 2.5 HPA consumer

Standard K8s pattern; we ship a sample ClusterRoleBinding to grant `system:serviceaccount:kube-system:horizontal-pod-autoscaler` access to our metric paths:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: { name: ollie-custom-metrics-reader }
rules:
  - apiGroups: [custom.metrics.k8s.io]
    resources: ["*"]
    verbs: [get, list]
```

## 3. Kernel privileges (agent)

The agent is the only component with kernel privileges. Per [`data-plane.md`](data-plane.md) §7:

| Capability | Why |
|---|---|
| `CAP_BPF` | load eBPF programs, create maps |
| `CAP_PERFMON` | access perf events, ringbuffers |
| `CAP_NET_ADMIN` | required for some BPF program types (e.g. socket-filter) |
| `hostPID: true` | required to attach uprobes to PIDs in other pod namespaces |

We deliberately **do not request `CAP_SYS_ADMIN`**. Kernel ≥ 5.8 split the relevant capabilities; using only the narrow caps reduces blast radius. If a specific OBI feature requires `CAP_SYS_ADMIN`, we either fork around it per [ADR-0010](decisions.md#adr-0010-obi-version-pinning-and-adapter) or document and gate the feature as opt-in.

Read-only host mounts: `/sys/fs/bpf` (rw, for map pinning), `/proc` (ro), `/sys/kernel/debug` (ro, for BTF).

`readOnlyRootFilesystem: true` on the container; `runAsUser: 0` (eBPF load requires uid 0 in the user namespace).

## 4. Security / threat model

A node-privileged DaemonSet that decrypts TLS payloads is a high-value target. This section makes the threat model explicit.

### 4.1 What an attacker who compromises the agent gains

If the agent process is compromised (RCE in the agent binary or any dependency it loads at runtime):

- **Read access to all traffic on the node**, including TLS-decrypted L7 for supported libraries, on every pod running there. This is the worst-case bear we're hugging.
- **Ability to load arbitrary eBPF programs** (within `CAP_BPF` + `CAP_PERFMON`). Modern eBPF verifiers prevent kernel memory access, but this still enables sophisticated side-channels.
- **Ability to read `/proc/<pid>/*`** for every PID on the node — environment variables, command lines, mount namespaces.
- **Ability to call Kubelet on the node** with the SA's privileges (`nodes/proxy`, which is broad).

What they **do not** gain directly:
- K8s API write access (agent has read-only outside of its own SAR/SSAR-tier resources).
- Cross-node access (no inter-node call paths from the agent; agent only talks to controller and configured sinks).
- Persistent foothold beyond the pod lifecycle (read-only root FS; the WAL volume is the only writable space).

### 4.2 What an attacker who compromises the controller gains

- **Authority to push arbitrary `MonitoringSpec` to every agent** — they can turn on TLS decryption everywhere, set 100% sampling, point spans at any in-cluster sink referenced in the SinkCatalog ConfigMap.
- **The cluster-wide K8s API read access** the controller has (Pods, Services, EndpointSlices, CRDs).
- **Validating-webhook position** — could approve malicious CRs from less-privileged users.

The controller does **not** have direct access to captured data; that lives on the agents.

### 4.3 What an attacker who compromises the query server gains

- **All recent in-cluster captured data**, via fan-out queries.
- Nothing else — no K8s API access, no write paths.

### 4.4 Mitigations

| Concern | Mitigation |
|---|---|
| Agent compromise → cluster-wide TLS read | Default `ClusterTrafficPolicy` has TLS **disabled**; opt-in per workload. Operators can refuse to enable TLS capture cluster-wide. |
| Controller compromise → spec injection | `MonitoringSpec.ExtraSinks` references **names**, not configs. Sinks must be pre-registered on agents via install-time config; the controller cannot inject sink configs. |
| Controller compromise → exfil via spec | Specs go only to agents over the gRPC stream; agents do not push raw events to the controller. The controller cannot turn into an exfil sink without operator install action. |
| Webhook compromise | `failurePolicy: Fail`; webhook restart is loud (CRDs become un-applyable). |
| Query-server compromise → data theft | All API surfaces require auth (custom-metrics-API uses K8s aggregation auth; gRPC streaming requires mTLS, configurable). |
| Supply chain (image, deps) | Reproducible builds via distroless base + CGO_ENABLED=0 + `go.sum` pinning. SBOMs published per release. SLSA Level 3 target. |
| Lateral via Kubelet `nodes/proxy` | This is the broadest privilege the agent has. We intend to evaluate moving local pod discovery to a less-privileged API (the upcoming kubelet-podresources gRPC API) once it covers our needs. |
| Eavesdrop on controller↔agent stream | TLS on the controller Service (cert-manager); agent verifies. Future: mTLS with per-node client certs. |

### 4.5 Audit and forensics

- Every CR change is in the K8s audit log.
- The controller emits an audit-style log line for each `MonitoringSpec` it pushes, including the user/SA that triggered the CR change (from the CR's `metadata.managedFields`).
- Sink registration changes are logged at install / config-reload time.
- The agent emits a structured audit line on every `EnableModule` call (especially for TLS modules).

### 4.6 NetworkPolicy

Default install ships NetworkPolicy YAML (not auto-applied; operator opt-in):

```yaml
# Egress: agent → controller (gRPC), agent → configured sinks (operator-set list)
# Ingress: agent ← query-server (fan-out queries)
# Egress: controller → kube-apiserver
# Egress: query-server → agent
# Ingress: query-server ← kube-apiserver (custom-metrics-API)
```

`ollie-system` is a closed-by-default namespace from a network perspective.

### 4.7 Reporting vulnerabilities

`SECURITY.md` at repo root will document the process. Threat-class-1 reports (RCE, sandbox escape) get a 24h ack; threat-class-2 (info disclosure) 72h.

## 5. Self-observability

Every component exposes Prometheus metrics on `/metrics` on its main port (or a dedicated port for the agent, since it doesn't have an HTTP user-API). Metric names are prefixed `ollie_<component>_*`.

The full metric catalog is in the respective component design docs; here's the per-component summary:

| Component | Metric prefix | Endpoint |
|---|---|---|
| Agent | `ollie_agent_*` + subsystem-specific (`store`, `topology`, `sink`, `capture`) | `:9090/metrics` |
| Controller | `ollie_controller_*` | `:9090/metrics` |
| Query server | `ollie_query_*` | `:9090/metrics` (separate from `:8443` user surface) |
| Webhook | `ollie_webhook_*` | `:9090/metrics` |

A bundled Grafana dashboard JSON is published per release covering:
- Capture rate per module per node
- Store HEAD size, WAL fsync latency, compaction rate
- Sink success/drop ratio per sink
- Controller reconcile rate + errors
- Identity cache size + delta rate
- HPA query latency (custom-metrics-API)

### 5.1 Health probes

```yaml
readinessProbe: { httpGet: { path: /healthz/ready, port: 9090 } }
livenessProbe:  { httpGet: { path: /healthz/live,  port: 9090 } }
```

`/healthz/ready` returns 200 only when the component has finished bootstrap (agent: capture started, store opened, controller stream connected OR fallback informer hot; controller: leader-elected OR follower with informer cache synced; query: at least one agent registered).

`/healthz/live` returns 200 unless the process is wedged (deadlock detection via a watchdog goroutine; goroutine count above ceiling; persistent panic loops).

### 5.2 Tracing

All components self-trace via OTel SDK. Spans emit to a configurable OTLP endpoint (default: empty / disabled). Sampling default 1%. Spans named per the `obs` package idiom in the existing repo.

## 6. Upgrade procedures

### 6.1 Version skew policy

Per [`public-api.md`](public-api.md) §6:

- Agent vs controller: N, N-1 (one MINOR version behind, in either direction, is supported).
- Controller vs CRD storage version: one storage version migration per controller release; conversion webhooks handle the prior version.
- Library users (third-party embedders): pin to one MINOR; bumps follow the same Stable/Experimental policy.

### 6.2 Upgrade order

1. **CRDs.** Apply new CRD YAML. New fields are additive in MINOR; storage version bumps require the conversion webhook from the previous release to still be running.
2. **Controller.** Rolling restart (2 replicas → 1 leader + 1 standby; leader election handles failover). The new controller version must understand the old storage version.
3. **Query server.** Rolling restart. Stateless; no migration.
4. **Agent (DaemonSet).** Rolling restart, one node at a time per `RollingUpdate.maxUnavailable: 1` (the default for DaemonSet).
5. **Verify.** Health probes; `ollie_*_version` gauges reflect new versions; sample CRD apply round-trips.

### 6.3 Downgrade

Supported only between consecutive MINOR versions. CRD storage version may need manual migration if the downgrade crosses a storage-version boundary; documented per release.

### 6.4 Helm / kustomize

Both shipped. Helm chart values map 1:1 to the operator-tunable knobs in `ollie-config`. Kustomize bundle is the source of truth; the Helm chart wraps it.

## 7. Debug surfaces

Every component exposes:

- **`/debug/pprof/*`** — Go pprof (gated to localhost or via `--enable-pprof-on-cluster-network=true` for non-prod investigation).
- **`/debug/config`** — current effective config (with secrets redacted).
- **`/debug/state`** — component-specific live state. Agent: active MonitoringSpecs, PID cache size, module status. Controller: active agent sessions, identity cache size, last reconcile timestamps. Query: registered sinks, recent query summary.
- **`SIGUSR1`** — reload config from disk (where applicable).
- **`SIGUSR2`** — dump goroutine stack to stderr (useful in liveness-probe-failure investigations).

A `iobsctl` CLI under `cmd/iobsctl/` wraps common operator queries — list active monitors, query metric, stream spans with a filter, dump identity cache.

## 8. Resource defaults

| Component | Requests | Limits |
|---|---|---|
| Agent (per node) | `100m` CPU / `200Mi` mem | `500m` CPU / `400Mi` mem |
| Controller (each replica) | `50m` CPU / `100Mi` mem | `500m` CPU / `512Mi` mem |
| Query server (each replica) | `100m` CPU / `200Mi` mem | `1` CPU / `1Gi` mem |
| Webhook | `25m` CPU / `64Mi` mem | `100m` CPU / `128Mi` mem |

Defaults sized for a 50-node cluster with ~500 pods. Operator overrides via Helm values or kustomize patches.

Agent's storage: `hostPath` `/var/lib/ollie` (cleared on node reboot is acceptable — it's a cache). Provisioned: ~1 GB recommended on the host.

## 9. Failure response runbook (operator-facing)

Published as `docs/runbook.md` alongside the design docs (not in this design doc, since it's operator material). Topics:

- "Agent OOMs on node X" → checks, common causes (cardinality spike), mitigation.
- "HPA isn't scaling on my custom metric" → checks (APIService Available, metric appears in `/apis/custom.metrics.k8s.io/...`, PromQL returns non-empty).
- "TrafficMonitor stuck Pending" → controller logs, webhook reachable, generation reconciliation.
- "Identity cache stale (peer shows as External when it shouldn't)" → controller informer health, fallback informer activation.
- "Agent CPU spike" → likely cardinality; check templating rules.
- "Sink dropping batches" → sink endpoint health, retry budget.

## 10. Documentation deliverables alongside the install

- `README.md` for the AP root with quickstart.
- Helm chart `values.yaml` with every tunable documented inline.
- `docs/runbook.md` (per §9).
- `docs/upgrade-X-to-Y.md` per MINOR upgrade.
- Generated CRD reference (kubebuilder-generated docs).

## Open questions

1. **mTLS for controller↔agent.** Currently TLS one-way (agent verifies controller). Per-node client certs (issued via cert-manager + ClusterIssuer) would be stronger; operationally heavier. Defer to v1.x once cert-rotation tooling is solid.
2. **kubelet-podresources API.** Replacing `nodes/proxy` Kubelet access with the narrower podresources gRPC API would reduce the agent's privilege surface meaningfully. Track upstream API maturity.
3. **eBPF program signing.** OBI's eBPF object files are signed by OBI maintainers (in theory). We do not currently verify signatures at load time. Worth investigating as supply-chain hardening.
4. **NetworkPolicy: ship as default?** Currently opt-in (provided as YAML, not applied). Should we apply by default in the Helm chart with a `disableNetworkPolicy: false` knob? Likely yes; defer to first install-experience iteration.
5. **SLSA level.** Target Level 3 for v1; concrete CI work to reach it is tracked separately.
