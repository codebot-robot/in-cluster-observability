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
