---
title: "Roadmap"
linkTitle: "Roadmap"
weight: 40
description: "What's shipped, what's coming, and what's deferred to GA."
---

The project is on a milestone cadence — `v0.1` → `v0.2` → `v0.3` → ... → `v1.0`. Each milestone branch in upstream is the integration point for that milestone's work, and milestone PRs stack (each one PRs to the previous) so the bottom of the stack collapses into `main` once a milestone is ready to ship.

Issue numbers below link to upstream `gke-labs/in-cluster-observability`.

## Shipped

### v0.1 Foundation — single Go module, public-package skeletons

Issues [#64–#69](https://github.com/gke-labs/in-cluster-observability/milestone/1). Established the single AP root, the `pkg/{capture,store,query,sink,topology,controller,schema,obsapi}` public API skeletons with stability tags, the OBI adapter shell, the distroless agent image, and the minimal DaemonSet manifest. Nothing observable yet — the binary stays alive on signals; that's it.

### v0.2 Capture MVP — OTLP receivers + OBI config writer + AllowPID

Issues [#70–#77](https://github.com/gke-labs/in-cluster-observability/milestone/2). The agent grew the OTLP gRPC + HTTP receivers (loopback), the atomic OBI config writer, `AllowPID`/`BlockPID` with a reload coalescer, L4 TCP + HTTP/1.1 translation, OTel self-obs metrics with panic-recovery + `ModuleDegraded` events, and the loopback debug HTTP endpoint. DaemonSet became two-container (obi sibling + agent).

### v0.3 (lean) — agent + OBI native enrichment

Issues from the [v0.3 milestone](https://github.com/gke-labs/in-cluster-observability/milestone/3), reframed by [ADR-0021](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md#adr-0021-lean-v03--agent-re-uses-obis-native-enrichment). The original "Storage MVP" scope (rebuilding K8s metadata, scrape, and a metric store on top of OBI) got abandoned once smoke testing showed OBI v0.9's informer already produces a superset of the K8s labels we'd planned to attach ourselves, and its OTel-shaped output didn't need our metric-name rewriting.

What landed instead:

- OBI v0.9 config schema fixed in `internal/obiconfig` (the `discovery.instrument` shape; `open_ports` as a string).
- `--obi-instrument-ports` smoke flag — the v0.3 way to drive OBI's discovery before the v0.4 controller exists.
- Translator passes `k8s.*` / `service.*` attrs through unchanged.
- Always-on Prometheus exporter on `--scrape-addr=:9090` + a forwarder that re-emits OBI's translated metrics through the same OTel SDK Meter (single scrape URL per node).
- DaemonSet caps + env + mounts fixed: `OTEL_EBPF_CONFIG_PATH` (not `--config=...`), L7 caps (`SYS_PTRACE`, `CHECKPOINT_RESTORE`, `DAC_READ_SEARCH`), `OTEL_EBPF_KUBE_METADATA_ENABLE=autodetect`, `/var/run/obi` + `/sys/fs/cgroup` mounts.
- RBAC manifest for OBI's K8s metadata informer.
- `ollie_agent_up{version}` boot gauge so the scrape path is verifiable without traffic.

See [What works today]({{< relref "what-works-today.md" >}}) for the concrete metric inventory.

## Next

### v0.4 Control Plane MVP

[Milestone](https://github.com/gke-labs/in-cluster-observability/milestone/4). Replaces the `--obi-instrument-ports` flag with declarative onboarding:

- `TrafficMonitor` (namespaced) and `ClusterTrafficPolicy` (cluster-scoped) CRDs.
- Controller Deployment with leader election (Lease-based).
- Validating admission webhook (schema + semantic checks).
- Controller↔agent bidirectional gRPC stream pushing `MonitoringSpec` per node.
- Reconciliation: CR → `MonitoringSpec` → agent → `AllowPID` → OBI config writer.
- CR status reporting (`matchedPodCount`, `Ready` condition, conflict detection).
- Least-privilege RBAC for the new ServiceAccounts (agent, controller, webhook).

### v0.5 Sinks + Query + HPA

[Milestone](https://github.com/gke-labs/in-cluster-observability/milestone/5). Closes the loop with the in-cluster store and the consumer-facing APIs:

- Prometheus `tsdb` HEAD wired as the in-cluster metric store ([#78](https://github.com/gke-labs/in-cluster-observability/issues/78)).
- WAL + periodic snapshot ([#79](https://github.com/gke-labs/in-cluster-observability/issues/79)).
- Span/edge ring buffer with persistence ([#84](https://github.com/gke-labs/in-cluster-observability/issues/84)).
- Query server Deployment + Service.
- PromQL fan-out across agents.
- `custom.metrics.k8s.io` APIService for HPA scaling on captured metrics.
- OTLP gRPC push sink, OTLP HTTP push sink.
- gRPC streaming sink with CEL filter compilation (for AI-agent consumers).
- `iobsctl` CLI for ad-hoc `logs | spans | metrics` queries.
- Identity broadcast (controller's canonical informer streams identity deltas to agents).
- Agent local fallback informer.

### v0.6 Hardening — HTTP/2, gRPC, TLS

[Milestone](https://github.com/gke-labs/in-cluster-observability/milestone/6). Protocol coverage to match the requirements:

- HTTP/2 module.
- gRPC module (with `rpc_*` attributes).
- TLS uprobes for Go `crypto/tls` and OpenSSL.
- Path templating engine (RE2 rules from `TrafficMonitor.spec.cardinality.pathTemplating`).
- Head-based sampling enforcement.
- Cardinality caps (peer.ip collapse, `MaxSamplesPerHEAD`).
- Aggregation hints honored in the HPA adapter.
- Cardinality self-obs metrics.

### v1.0 GA

[Milestone](https://github.com/gke-labs/in-cluster-observability/milestone/7). Production-ready release:

- Helm chart (and kustomize bundle).
- Full user-facing README + quickstart.
- Operator runbook (failure-response cookbook).
- Upgrade guide.
- Bundled Grafana dashboard.
- Performance budgets in CI (agent ≤ 5% CPU, ≤ 200MB RSS).
- Kernel matrix CI (cos-125 + GKE stable + arm64).
- Multi-arch images.
- NetworkPolicy in the Helm chart by default.

## Beyond v1.0

Items in the longer-term roadmap (per [`docs/design/roadmap.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/roadmap.md)) include Kafka protocol parsing, additional TLS library uprobes (rustls, NSS, JSSE), multi-cluster federation, a first-party UI, and long-term storage adapters beyond OTLP/Prometheus (ClickHouse, object-storage tiers).

## See also

- [`docs/requirements.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/requirements.md) — the agreed requirements catalog.
- [Upstream milestones](https://github.com/gke-labs/in-cluster-observability/milestones) — live status.
