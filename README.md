# Ollie

*Transparent eBPF network observability for Kubernetes workloads.*

Ollie captures L4 and L7 traffic — TCP, HTTP/1.1, HTTP/2, gRPC, and TLS-decrypted L7 — without modifying the workloads themselves, attaches Kubernetes identity to every record, stores recent data in-cluster for low-latency queries (HPA, AI agents), and exports to pluggable long-term sinks via OTLP, Prometheus, or a registerable Go sink interface.

The name plays on **o11y**, the standard observability abbreviation.

## Status

This repository is in the middle of a planned rewrite. The legacy POC code (Prometheus + eBPF agent at the repo root, OpenTelemetry sink/query pipeline under `opentelemetry/`, the `obs/` logging library) has been removed; it remains on `main` and is reachable via `git log main`.

Active development lives on the `rewrite` branch. The **v0.1 Foundation** milestone has landed (scaffolding, public API skeletons, OBI adapter shell, container image, minimal DaemonSet) — the agent deploys but does nothing observable yet. Real eBPF capture lands with v0.2; HPA-ready custom metrics with v0.5; production-ready v1.0 follows.

Milestones are tracked at [github.com/gke-labs/in-cluster-observability/milestones](https://github.com/gke-labs/in-cluster-observability/milestones).

## Where to read what

| You want to | Read |
|---|---|
| Understand what we're building | [`docs/requirements.md`](docs/requirements.md) |
| Understand how we're building it | [`docs/design/architecture.md`](docs/design/architecture.md) |
| Read the decision log | [`docs/design/decisions.md`](docs/design/decisions.md) |
| Set up your environment / contribute | [`AGENTS.md`](AGENTS.md), then [`docs/contributing.md`](docs/contributing.md) |
| Browse the roadmap | [`docs/design/roadmap.md`](docs/design/roadmap.md) |

A polished user-facing README ships with v1.0 ([issue #113](https://github.com/gke-labs/in-cluster-observability/issues/113)). This version is the in-flight pointer.

## Module path

The Go module path remains `github.com/gke-labs/in-cluster-observability` — the repository name. Within the project, binary, image, namespace, and metric prefix are all `ollie`.

## License

Apache 2.0 — see [`LICENSE`](LICENSE).

## Disclaimer

This is not an officially supported Google product.

This project is not eligible for the Google Open Source Software Vulnerability Rewards Program.
