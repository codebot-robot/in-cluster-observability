---
title: Documentation
linkTitle: Documentation
weight: 1
menu:
  main:
    weight: 10
---

You're in the Ollie reference docs. The site root has the project pitch; this section is the user-facing reference for the v0.3 dev preview.

## Start here

**Brand new?** → [Getting started]({{< relref "getting-started.md" >}}) walks from a fresh Kind cluster through deploying nginx and seeing per-pod HTTP metrics on the agent's `:9090` scrape endpoint, all with K8s identity attached.

**Want to understand what's actually shipping?** → [What works today]({{< relref "what-works-today.md" >}}) is the honest v0.3 inventory — what's captured, what the metric names look like, what isn't built yet.

**Curious about the architecture?** → [Architecture]({{< relref "architecture.md" >}}) covers the sibling-container model: the upstream OBI image runs as a sibling to the agent, the agent writes OBI's config + receives its OTLP + re-exposes everything on a single Prometheus URL.

**Looking ahead?** → [Roadmap]({{< relref "roadmap.md" >}}) lays out v0.4 (control plane), v0.5 (in-cluster store + HPA), and v0.6 (TLS + cardinality controls).

## Reference index

### Getting things done
- **[Getting started]({{< relref "getting-started.md" >}})** — deploy on Kind, see L4 + L7 metrics from a real workload in under 10 minutes.
- **[What works today]({{< relref "what-works-today.md" >}})** — feature snapshot for v0.3 with the metric names you'll actually see.

### Concepts and design
- **[Architecture]({{< relref "architecture.md" >}})** — sibling-container model, OBI's role, the agent's role, the OTLP → Prometheus path.
- **[Roadmap]({{< relref "roadmap.md" >}})** — what's coming v0.4–v1.0.

### Working on it
- **[Contributing]({{< relref "contributing.md" >}})** — branch model, commit style, where to file issues.

## Deeper reading

The repo's [`docs/design/`](https://github.com/gke-labs/in-cluster-observability/tree/main/docs/design) directory holds the full architectural design log: per-subsystem design docs, the architectural decision record ([`decisions.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md) — 21 ADRs and counting), and the agreed requirements ([`docs/requirements.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/requirements.md)). Read those if you're considering contributing or want to understand *why* the current architecture is what it is.
