# AGENTS.md

This file documents conventions and operational context for working in this repository. Both humans and agentic coding tools should read it before non-trivial changes.

## Project context

Lightweight in-cluster observability spike: a Kubernetes DaemonSet agent that gathers signals (network bytes from `/proc/net/dev`, plus TCP/HTTP counts via eBPF) and exposes Prometheus `/metrics`, alongside an OpenTelemetry-based pipeline (node sink + central query server) that also serves as a `custom.metrics.k8s.io` API for HPA. The goal is "enough signal for autoscaling/traffic management" with minimum overhead — prefer the standard library and a small dependency set.

> **Note:** all code currently in the repo is throwaway POC. See `docs/requirements.md` and `docs/rough_design.md` for the actual target. The architectural summaries below describe the *current* code so you can navigate it, not the system we're building.

## Issue and PR tracking

This clone is a fork. `origin` is `mastersingh24/in-cluster-observability`; `upstream` is `gke-labs/in-cluster-observability`. **Issues and milestones are tracked in `upstream`, not the fork** — `gh repo set-default` is configured accordingly, so `gh issue list`, `gh issue create`, `gh milestone …` operate against `gke-labs/in-cluster-observability` by default. PRs are the standard fork → upstream flow.

When opening or referencing issues, always link to the upstream issue number. Do not open or comment on issues in the fork's tracker.

## Repository layout — three AP roots, three Go modules

This repo uses [`ap`](https://github.com/gke-labs/gke-labs-infra/tree/main/ap) (autoproject) with **multiple AP roots**. An AP root is any directory containing a `.ap/` directory:

- **`/` (root)** — module `github.com/gke-labs/in-cluster-observability`. The original Prometheus + eBPF DaemonSet agent (`main.go`, `bpf/metrics.bpf.c`, generated `metrics_bpf{el,eb}.{go,o}`). Deployed via `k8s/manifests.yaml` → image `in-cluster-observability-agent`.
- **`opentelemetry/`** — module `.../opentelemetry`. The OTLP pipeline (see below). Has its own `k8s/manifest.yaml` deployed to namespace `observability-system`.
- **`obs/`** — module `.../obs`. A small library wrapping `logr` + OpenTelemetry so spans act as logging contexts and attributes flow to both the span and the logger. See `obs/llms.txt` for the API shape.

Each AP root has its own `images/<name>/Dockerfile`; the build context for an image is **its AP root**, not the repo root. Image names map to directory names — `ap` handles registry prefixing.

## Building, testing, linting

Always invoke `ap` via `go run`, never assume it's installed globally:

```
go run github.com/gke-labs/gke-labs-infra/ap@latest <command>
```

| Task                | Command                                            |
|---------------------|----------------------------------------------------|
| Generate files      | `ap generate //...`                                |
| Lint                | `ap lint //...`                                    |
| Unit tests          | `ap test //...`                                    |
| Build images        | `ap build //...`                                   |
| E2E (root agent)    | `ap e2e .`                                         |
| E2E (otel pipeline) | `ap e2e opentelemetry`                             |
| Deploy              | `ap deploy` (from an AP root)                      |

CI runs these via thin wrappers in `dev/ci/presubmits/` (`ap-build`, `ap-lint`, `ap-test`, `ap-e2e`, `ap-e2e-opentelemetry`, `ap-verify-generate`). If `ap build` fails in CI, **run it locally before claiming it passes** — GEMINI.md is explicit about this.

`ap-verify-generate` will fail if `ap generate //...` produces a diff; the hint there is the fix.

Standard `go test ./...` works inside any module for quick iteration, but `ap test` is the source of truth (it knows about all three modules). E2E tests are gated by `RUN_E2E` env var and require a Kind cluster — see `tests/e2e/harness.go` and `opentelemetry/tests/e2e/harness.go`.

Run a single Go test the usual way, e.g. `cd opentelemetry && go test ./cmd/opentelemetry-sink -run TestWriter`.

## Kubernetes manifest conventions (enforced by `ap`)

- Manifests live in a `k8s/` directory inside the AP root.
- **Do not set `imagePullPolicy`** unless there's a specific reason — `ap deploy` manages it.
- Image references should be the bare image name (e.g. `opentelemetry-node-agent`); `ap` adds the registry prefix at deploy time.

## OpenTelemetry pipeline architecture (`opentelemetry/`)

The flow is **node-local OTLP sinks → central query server → consumers (HPA / `otelctl`)**:

1. **`opentelemetry-node-agent`** (DaemonSet, `cmd/opentelemetry-sink/`) — receives OTLP on `:4317` (gRPC) and `:4318` (HTTP). Writes all traces/metrics/logs to a node-local binary file in a **kOps-compatible format**: a 16-byte header (length, CRC32, flags, TypeCode) followed by the marshaled proto. `TypeCode 1` is an `ObjectType` message that maps subsequent codes to proto type names. See `cmd/opentelemetry-sink/README.md` and `AGENTS.md` for design rationale (local storage chosen to avoid memory overhead).
2. **Registration**: each sink streams a `Register` RPC to the query server (default `queryserver.observability-system:9443`) advertising its own `POD_IP:4317`, heartbeating every 5s. The query server maintains a `Registry` of live sinks.
3. **`opentelemetry-query-server`** (Deployment, `cmd/opentelemetry-query-server/`) — fans out queries to all registered sinks in parallel, aggregates results. Exposes:
   - `/query` HTTP — generic query (filter string).
   - `/apis/custom.metrics.k8s.io/v1beta1/...` — implements the Kubernetes custom metrics API so HPAs can scrape pod metrics. Registered cluster-wide via an `APIService` object (TLS via cert-manager).
   - gRPC `FrontendQueryService` — used by `otelctl`; takes CEL expressions as filters, compiles them against OTLP proto types, evaluates per-record.
4. **`otelctl`** (`cmd/otelctl/`) — CLI that port-forwards to the query server pod and runs `logs|traces|metrics [cel-filter...]` queries.
5. **`test-app` / `test-client`** — load generator pair used by e2e tests (`test-app` emits a `qps` OTel gauge; `test-client` drives traffic at a target QPS).

Protos live in `opentelemetry/proto/` and are generated into `opentelemetry/pkg/pb/` (regenerate via `ap generate`).

## Root agent specifics (`main.go` + `bpf/`)

- eBPF source in `bpf/metrics.bpf.c`; the `go:generate` directive in `main.go` uses `cilium/ebpf`'s `bpf2go` to produce `metrics_bpf{el,eb}.{go,o}`. These generated files **are checked in** and verified by `ap-verify-generate`.
- Probes attached at startup: kprobes on `tcp_v4_connect` / `inet_csk_accept`, tracepoint on `sys_enter_write` (rough HTTP GET detection by sniffing the buffer). Counters live in BPF hash maps polled every 5s and copied into Prometheus counters.
- Requires Linux + privileged container (mounts host `/proc` and `/sys`); the local `go run main.go` path will only work on Linux.

## Coding conventions

- Standard Go idioms; minimize dependencies (prefer stdlib or established lightweight packages).
- Apache 2.0 copyright header on every Go and shell file (`copyrightHolder: Google LLC`, see `.ap/headers.yaml`). YAML files are skipped.
- Add Prometheus or OTel metrics for new functionality where applicable.
- For Go logging/tracing inside `opentelemetry/`, prefer the `obs` package idiom (`ctx, span := obs.Start(ctx, "name", obs.String(...))`) — see `obs/llms.txt`.
