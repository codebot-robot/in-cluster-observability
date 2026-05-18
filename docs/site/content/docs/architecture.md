---
title: "Architecture"
linkTitle: "Architecture"
weight: 30
description: "How the agent and OBI fit together — the sibling-container model and why the agent is intentionally thin."
---

Ollie is built on top of [OpenTelemetry eBPF Instrumentation (OBI)](https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation). OBI does the eBPF capture and K8s metadata enrichment; Ollie adds the production deployment scaffolding plus an OTLP receiver carve-out that future milestones use as the hook point for CRD-driven onboarding (v0.4) and the in-cluster store (v0.5).

This page describes the data path that's actually shipping in v0.3. For the full design log and the reasoning behind each choice, see the [ADRs in the repo](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md) — especially [ADR-0018](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library) (sibling-container model) and [ADR-0021](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md#adr-0021-lean-v03--agent-re-uses-obis-native-enrichment) (lean v0.3 pivot to OBI native enrichment).

## The two-container pod

Each node runs a DaemonSet pod with two containers sharing the pod's network namespace and an `emptyDir` volume:

```
┌──────────────────────────────────────────────────────────────┐
│ DaemonSet pod (ollie-agent)                                  │
│                                                              │
│  ┌───────────────────────┐    ┌─────────────────────────┐    │
│  │ obi (sibling)         │    │ agent (this project)    │    │
│  │ otel/ebpf-instrument  │    │ ollie:v0.3              │    │
│  │ :v0.9.0               │    │                         │    │
│  │                       │    │                         │    │
│  │ eBPF capture          │    │ OBI config writer       │    │
│  │  - L4 socket filter   │    │  ──> /etc/ollie/...     │    │
│  │  - L7 uprobes         │    │                         │    │
│  │                       │───▶│ OTLP receiver (loopback)│    │
│  │ K8s metadata informer │    │ Forwarder ──> OTel Meter│    │
│  │  - list/watch pods,   │    │ Prometheus exporter     │    │
│  │    services, nodes,   │    │  ──> :9090/metrics      │    │
│  │    replicasets        │    │                         │    │
│  └───────────────────────┘    └─────────────────────────┘    │
│             ▲                              │                 │
│             │ config file (atomic write)   │                 │
│             └──────────────────────────────┘                 │
│                                                              │
│  Shared: emptyDir at /etc/ollie/obi-config/                  │
│  Shared: pod network namespace (loopback OTLP)               │
└──────────────────────────────────────────────────────────────┘
```

**OBI (sibling container)** does the kernel work:

- L4 TCP captured via an in-kernel socket filter — fires on every packet, doesn't need to know what process owns the socket. Gives you `obi_network_flow_bytes` with `direction`, `k8s_src_*`, `k8s_dst_*` labels for free.
- L7 HTTP/1.1 captured via uprobes attached to processes whose listening port matches the configured selector (`--obi-instrument-ports` on the agent, which writes the OBI `discovery.instrument` block). Gives you `http_server_request_duration` and friends, one series per request shape.
- K8s metadata informer running inside the OBI container — watches pods/services/nodes/replicasets with the RBAC granted by `k8s/rbac.yaml` and attaches `k8s.pod.name`, `k8s.namespace.name`, `k8s.deployment.name`, etc. to every captured event.
- Privileged. Holds `CAP_BPF`, `CAP_PERFMON`, `CAP_NET_RAW`, `CAP_SYS_PTRACE`, `CAP_CHECKPOINT_RESTORE`, `CAP_DAC_READ_SEARCH`. The L7 cap set is what lets it cross PID namespaces and read target ELFs; without those, Application mode silently no-ops.

**Agent (our container)** is intentionally thin:

- **OBI config writer.** Watches its in-process state (PIDs allow-listed via the future controller's gRPC stream, plus the `--obi-instrument-ports` seed) and atomically writes `/etc/ollie/obi-config/config.yaml`. OBI's built-in file watcher picks up the change and reloads. Implementation in [`internal/obiconfig`](https://github.com/gke-labs/in-cluster-observability/tree/main/internal/obiconfig).
- **OTLP receiver on loopback.** OBI's OTLP exporter is pointed at `http://127.0.0.1:4318`, so all OBI-captured events flow through the agent. This is the carve-out hook point that v0.4's CRD-driven filtering and v0.5's in-cluster store will use.
- **Metric forwarder.** Each translated `MetricEvent` is re-recorded into the agent's OTel SDK MeterProvider via a name-suffix-heuristic forwarder (counters for `*.total / .count / .bytes / .duration / .size`; gauges otherwise). See [`cmd/ollie/forward.go`](https://github.com/gke-labs/in-cluster-observability/blob/main/cmd/ollie/forward.go).
- **Prometheus exporter on `:9090`.** Same OTel SDK MeterProvider, single scrape URL.
- Unprivileged. `runAsUser: 65532`, no Linux capabilities, read-only root filesystem.

The split has one real consequence: **OBI is the source of truth for K8s identity, not the agent.** The agent never runs its own K8s informer or PID cache — those would just duplicate what OBI already does. [ADR-0021](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md#adr-0021-lean-v03--agent-re-uses-obis-native-enrichment) records the pivot from an earlier design that had us doing both.

## The data path, in order

For a single HTTP request to nginx:

1. Client → nginx (port 80) on the cluster network.
2. Linux kernel sees the packets. OBI's L4 socket filter eBPF program records `obi_network_flow_bytes` with K8s identity attached by OBI's informer.
3. OBI's L7 uprobes (attached to nginx) record the request: method, path, status code, duration.
4. OBI emits OTLP HTTP to `http://127.0.0.1:4318` (the agent's loopback OTLP receiver).
5. Agent's translator (in [`pkg/capture`](https://github.com/gke-labs/in-cluster-observability/tree/main/pkg/capture)) walks the OTLP message and produces one `MetricEvent` or `SpanEvent` per datapoint.
6. Agent's forwarder records each `MetricEvent` into the OTel SDK MeterProvider with the original metric name and the full attribute set (including `k8s.*` from OBI's informer).
7. The OTel SDK Prometheus exporter renders the meter's state when `/metrics` is scraped on `:9090`.

`SpanEvent` and `EdgeEvent` flow through the same writer goroutine but are currently drained — the persistence layer arrives in v0.5.

## Why this shape

A few design properties to note, since they affect how you reason about the system:

- **Zero application code changes.** Workloads don't link an SDK, don't run a sidecar, don't get a mutating webhook. Every label on every metric comes from kernel + K8s API observation.
- **One scrape URL per node.** No second port for OBI's own metrics, no separate `/debug/metrics`. If you point Prometheus at the agent's `:9090`, you see everything.
- **OBI version pinning is image-tag-based, not Go-module-based.** The agent has zero `import` statements from any `go.opentelemetry.io/obi/*` package — enforced by a Go test in [`internal/archtest`](https://github.com/gke-labs/in-cluster-observability/tree/main/internal/archtest). Upgrading OBI is "change the image tag in `k8s/daemonset.yaml`, run the contract tests, ship."
- **The agent is thin on purpose.** Everything OBI already does well (kernel capture, K8s metadata, OTel-shaped output) is delegated. The agent's value-add is what OBI *doesn't* do: declarative CRD-driven onboarding (v0.4), an in-cluster store + cluster-local query (v0.5), HPA custom-metrics-API integration (v0.5), AI-agent streaming with CEL filters (v0.5), pluggable sinks (v0.5).

## See also

- [Getting started]({{< relref "getting-started.md" >}}) — concrete deploy on Kind.
- [What works today]({{< relref "what-works-today.md" >}}) — what you'll see on the scrape endpoint right now.
- [Roadmap]({{< relref "roadmap.md" >}}) — what's coming.
- [`docs/design/`](https://github.com/gke-labs/in-cluster-observability/tree/main/docs/design) in the repo — full design log, including per-subsystem docs.
