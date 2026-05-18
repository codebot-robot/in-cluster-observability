---
title: in-cluster-observability
---

{{< blocks/cover title="in-cluster-observability" image_anchor="top" height="med" >}}

<p class="lead mt-5">
Transparent, eBPF-based network observability for Kubernetes workloads. Zero instrumentation — your apps don't change. L4 TCP + L7 HTTP metrics and spans with full K8s identity attached automatically, scrapable by Prometheus on every node.
</p>

<a class="btn btn-lg btn-primary me-3 mb-4" href="docs/getting-started/">Get started <i class="fa-solid fa-arrow-right ms-2"></i></a>
<a class="btn btn-lg btn-secondary me-3 mb-4" href="https://github.com/gke-labs/in-cluster-observability">Source on GitHub <i class="fa-brands fa-github ms-2"></i></a>

{{< /blocks/cover >}}

{{% blocks/lead color="primary" %}}

`in-cluster-observability` is a "more flexible Pixie" — an eBPF-based agent that captures network traffic on every Kubernetes node and turns it into metrics, spans, and topology edges, attributed by K8s identity (pod, namespace, deployment, service). Built on top of [OpenTelemetry eBPF Instrumentation (OBI)](https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation), it adds a thin agent that exposes everything on a single Prometheus scrape endpoint — with declarative CRD-driven onboarding, pluggable sinks, and an in-cluster store on the roadmap.

**Status: v0.3 dev preview.** Capture pipeline works end-to-end on real clusters; CRD/controller, in-cluster store, custom-metrics API for HPA, and AI-agent streaming arrive in v0.4–v0.5. See [What works today]({{< relref "docs/what-works-today.md" >}}) for the honest current shape.

{{% /blocks/lead %}}

{{% blocks/section color="white" %}}

## Should I try this today?

`in-cluster-observability` is on a milestone cadence and v0.3 is the first release with anything user-observable end-to-end. Use this to self-select before you spend 10 minutes on the Kind walkthrough.

### Yes — try v0.3 today if …

- You're **evaluating the architecture / direction** and want a working demo on Kind to validate the thesis (zero-instrumentation L4 + L7 metrics with K8s identity, OBI as a sibling container, single Prometheus URL).
- You want **OBI's metrics pre-wired with K8s labels** without figuring out the OBI capability set, K8s informer RBAC, and `OTEL_EBPF_CONFIG_PATH` env var yourself — the manifest captures hard-won setup knowledge.
- You have **HTTP/1.1 workloads** to play with (nginx, simple HTTP services) and an existing Prometheus to scrape them.
- You want to **file issues or contribute** against the v0.4 controller, v0.5 store, or v0.6 hardening milestones.

### Come back for v0.4–v0.5 if …

- You want to **scope monitoring per-workload** via declarative `TrafficMonitor` CRDs (label selectors, not "every process on port 80"). → v0.4.
- You want **HPA scaling driven by captured network metrics** (the `custom.metrics.k8s.io` API). → v0.5.
- You want **in-cluster PromQL** over captured data without piping it out to Cortex / Mimir. → v0.5.
- You want **span / edge persistence** and a query path for spans (`iobsctl spans --filter '...'`). → v0.5.
- You want **AI-agent streaming** with CEL-filtered subscriptions. → v0.5.

### Come back for v0.6 / v1.0 if …

- Your workloads use **HTTP/2, gRPC, or TLS** — v0.3 covers L4 + HTTP/1.1 only; the rest lands in v0.6.
- You're worried about **cardinality blowups** from high-variability URL paths — path templating, sampling, and peer-IP collapse arrive in v0.6.
- You want a **Helm chart, operator runbook, upgrade guide, or multi-arch images** — those are v1.0 deliverables.
- You're **running this in production** — wait for v1.0; v0.3 has no upgrade path and no SLOs.

If you fit any of the "wait for" rows, the [roadmap]({{< relref "docs/roadmap.md" >}}) tracks expected scope per milestone, and starring / watching the [upstream repo](https://github.com/gke-labs/in-cluster-observability) is the easiest way to know when to revisit.

{{% /blocks/section %}}

{{% blocks/section color="dark" type="row" %}}

{{% blocks/feature icon="fa-solid fa-eye" title="Zero instrumentation" url="docs/architecture/" %}}
The eBPF data plane (OBI) attaches probes in the kernel — no SDK to import, no service-mesh sidecar, no code changes. Captures L4 TCP (bytes, RTT) via a socket filter and L7 HTTP/1.1 via uprobes against any process listening on the configured ports.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-brands fa-kubernetes" title="K8s identity, free" url="docs/what-works-today/" %}}
OBI's K8s metadata informer attaches `k8s.pod.name`, `k8s.namespace.name`, `k8s.deployment.name`, `k8s.container.name`, `k8s.node.name`, `k8s.pod.uid`, and friends to every metric and span automatically. L4 flows even get dual-sided attribution — `k8s_src_*` and `k8s_dst_*` on the same datapoint.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-chart-line" title="Prometheus-scrapable" url="docs/getting-started/" %}}
One scrape URL per node (`:9090/metrics`) exposes everything the agent sees — captured workload metrics plus its own self-observability counters. Standard OTel metric names; works with any Prometheus-compatible scraper.
{{% /blocks/feature %}}

{{% /blocks/section %}}

{{% blocks/section %}}

## Try it

```sh
# Build, load into Kind, deploy.
docker build -t ollie:v0.3 -f images/ollie/Dockerfile .
kind load docker-image --name ollie-v03 ollie:v0.3
kubectl apply -k k8s/

# Deploy nginx, drive traffic.
kubectl create namespace demo
kubectl create deployment nginx --image=nginx:1.27 -n demo
kubectl expose deployment nginx --port=80 -n demo

# Scrape — you'll see http_server_request_duration with k8s_pod_name=nginx-*
# attached, alongside L4 obi_network_flow_bytes with src+dst identity.
```

See [Getting started]({{< relref "docs/getting-started.md" >}}) for the full Kind walkthrough, [Architecture]({{< relref "docs/architecture.md" >}}) for the sibling-container model, or [What works today]({{< relref "docs/what-works-today.md" >}}) for the concrete v0.3 feature snapshot.

{{% /blocks/section %}}
