# AGENTS.md

This file documents conventions and operational context for working in this repository. Both humans and agentic coding tools should read it before any non-trivial change. Keep it current as conventions evolve.

`GEMINI.md` is a stub that points back here so the two tools see the same content.

## Current state

This repo is in the middle of a planned rewrite. The legacy POC code (Prometheus + eBPF agent at the root, OpenTelemetry sink/query pipeline under `opentelemetry/`, the `obs/` logging library) was removed early in the rewrite. POC code is preserved on `main` and reachable via `git log main -- <path>`.

All code lives at the **repo root** as a single AP root and single Go module `github.com/gke-labs/in-cluster-observability` (per [ADR-0015](docs/design/decisions.md#adr-0015-collapse-core-to-repo-root-supersedes-adr-0013-layout)). Per [ADR-0018](docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library), OBI runs as a **sibling container** in the agent DaemonSet pod, not as an embedded Go library — our agent is an OTLP receiver + OBI config writer.

Milestone status:

- **v0.1 Foundation** ([#64](https://github.com/gke-labs/in-cluster-observability/issues/64)–[#69](https://github.com/gke-labs/in-cluster-observability/issues/69)) landed: AP root, public package skeletons, OBI adapter shell, container image, minimal DaemonSet.
- **v0.2 Capture MVP** ([#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77)) landed: OTLP receivers (gRPC + HTTP loopback), OBI config writer, AllowPID/BlockPID with reload coalescer, L4 TCP + HTTP/1.1 translation, OTel self-obs metrics, panic recovery + ModuleDegraded events, debug HTTP endpoint, contract-test harness. DaemonSet now has two containers (obi sibling + agent).
- **v0.3 (lean) — agent + OBI native enrichment** (per [ADR-0021](docs/design/decisions.md#adr-0021-lean-v03--agent-re-uses-obis-native-enrichment), supersedes the original v0.3 "Storage MVP" plan). Adds: OBI v0.9 schema fixes in `internal/obiconfig` (`discovery.instrument` + `open_ports` string + `target_pids`), `--obi-instrument-ports` smoke-test seed, OTel SDK Prometheus exporter always-on with a forwarder that re-records OBI's translated metrics, `--scrape-addr` agent listener at `:9090`, RBAC for OBI's K8s metadata informer, DaemonSet refactor (right caps + `OTEL_EBPF_CONFIG_PATH` env var + `/var/run/obi` + `/sys/fs/cgroup` mounts). What we explicitly did *not* build: a separate `internal/pidcache`, `internal/enricher`, `pkg/sink/promscrape`, or `pkg/store.MetricStore` — OBI's K8s informer + the OTel SDK + this single forwarder replace all of them.
- **v0.4 Control Plane MVP** is next: CRDs (`TrafficMonitor` / `ClusterTrafficPolicy`), controller, gRPC stream pushing `MonitoringSpec` to agents, then the controller's `AllowPID` calls flow into the same OBI config writer the agent has today.

What's in the repo:

- `docs/` — design (`docs/design/`), agreed requirements (`docs/requirements.md`), early rough sketch (`docs/rough_design.md`)
- `AGENTS.md` — this file
- `GEMINI.md` — stub pointing here
- `.ap/` — autoproject config (`ap.yaml`, `headers.yaml`)
- `go.mod` — single Go module
- `cmd/ollie/` — default binary; v0.3 starts OTLP receivers (loopback) + OBI config writer + Prometheus scrape on `:9090` + an OBI-metrics → OTel-SDK forwarder. Optional loopback debug endpoint at `:9099`.
- `pkg/` — public API: `capture` (Manager + TranslateMetrics/TranslateTraces + NewPromMeterProvider), `obsapi`, `sink`, `topology`, `store` (interface stub; concrete span/edge store lands in v0.5), `query`, `controller`, `schema` (label-key + bucket constants)
- `internal/` — private packages: `obiconfig` (typed OBI YAML schema + atomic writer), `otlpreceiver` (loopback gRPC + HTTP OTLP receivers), `debugendpoint` (loopback PID-control HTTP), `archtest` (enforces OBI import boundary)
- `images/ollie/` — Dockerfile (distroless static, CGO disabled)
- `k8s/` — install manifests (namespace + RBAC + DaemonSet with `obi` + `agent` containers + kustomization)
- `tests/contract/obi/` — OBI adapter contract tests + fixture harness
- `dev/ci/presubmits/` — CI script wrappers
- `.github/workflows/` — CI YAML
- `LICENSE`, `README.md`, `.gitignore`

## Where to read what

| Topic | Doc |
|---|---|
| What we're building (agreed) | [`docs/requirements.md`](docs/requirements.md) |
| How we're building it | [`docs/design/architecture.md`](docs/design/architecture.md) (entry point) |
| Recorded design decisions | [`docs/design/decisions.md`](docs/design/decisions.md) |
| Per-subsystem design | other files in [`docs/design/`](docs/design/) |
| Roadmap (deferred items) | [`docs/design/roadmap.md`](docs/design/roadmap.md) |
| Issues + milestones | upstream `gke-labs/in-cluster-observability` (see Issue/PR tracking below) |

## Issue and PR tracking

This clone is a fork of `gke-labs/in-cluster-observability`. **All issues and milestones live in upstream**, not the fork. `gh repo set-default` is configured accordingly — `gh issue list`, `gh issue create`, `gh milestone …` target upstream by default. Always link to upstream issue numbers.

### Branch and PR workflow

**One integration branch per milestone**, named after the milestone: `v0.1`, `v0.2`, …, `v1.0`. PRs **stack**: each milestone branch PRs to the previous (`v0.2` → `v0.1`, `v0.3` → `v0.2`, …), with `v0.1` → `main` at the bottom of the stack. When a milestone PR merges to `main`, GitHub auto-updates the next PR's base — the stack collapses one PR at a time.

```
main ◄── PR ── v0.1 ◄── PR ── v0.2 ◄── PR ── v0.3 ── …
        (#125)         (next)         (next)
```

Rules:

- **Commit fine-grained on the active milestone branch.** One logically separable unit per commit. No WIP megacommits. Per-issue feature branches are allowed for large or risky work but not required by default.
- **Each milestone PR is the review gate** for that milestone's work.
- **Never commit directly to `main`.** Main only advances by merging the bottom of the stack.
- **Starting milestone vN.M:** `git checkout -b vN.M v<prev>` off the previous milestone branch's tip, `git push -u upstream vN.M`, then `gh pr create --base v<prev> --head vN.M`.
- **Hygiene fixups** for an in-flight milestone go on that milestone's branch (small commits extending its PR) — not on `main`, not on a later milestone branch.

## Build, test, lint

The project uses [`ap`](https://github.com/gke-labs/gke-labs-infra/tree/main/ap) (autoproject). Always invoke via `go run`:

```
go run github.com/gke-labs/gke-labs-infra/ap@latest <command>
```

| Task | Command |
|---|---|
| Generate files | `ap generate //...` |
| Lint | `ap lint //...` |
| Unit + contract tests | `ap test //...` |
| Build images | `ap build //...` |
| E2E (Kind required) | `ap e2e .` |

For quick local iteration, plain Go commands work too: `go build ./...`, `go test ./...`. `ap` is authoritative for any operation that touches generated files, manifests, or images.

CI runs the above via wrappers in `dev/ci/presubmits/`: `ap-build`, `ap-e2e`, `ap-lint`, `ap-test`, `ap-verify-generate`. If `ap build` fails in CI, **run it locally before claiming it passes**.

`ap-verify-generate` fails if `ap generate //...` produces a diff — the hint is the fix.

## Kubernetes manifest conventions (enforced by `ap`)

- Manifests live in `k8s/` at the AP root.
- **Do not set `imagePullPolicy`** unless there's a specific reason — `ap deploy` manages it.
- Image references should be the bare image name (e.g. `ollie`); `ap` adds the registry prefix at deploy.

## Apache 2.0 license headers

Every code/config artifact (Go, YAML, Dockerfile, proto, shell) carries the full Apache 2.0 license header with `Copyright 2026 Google LLC` at the top of the file. Auto-injected for Go and shell by `.ap/headers.yaml`; YAML, Dockerfile, and proto are added by hand (see `.ap/headers.yaml` for the `skip` list). Markdown is unannotated by repo precedent. `go.mod`, `go.sum`, `LICENSE`, and `.git*` files are also skipped.

## Coding conventions

- Standard Go; minimize dependencies (prefer stdlib or established lightweight packages). v0.1 keeps the module stdlib-only; third-party deps land with their consuming milestone.
- Self-observability metric names are prefixed `ollie_<component>_*` — see [`docs/design/operations.md`](docs/design/operations.md) §5. The `pkg/schema` package exports the canonical metric-name and label-key constants; reference those instead of string literals.
- Public Go API surface lives under `pkg/*` with explicit stability tags (`// Stability: Stable | Experimental | Internal`) — see [`docs/design/public-api.md`](docs/design/public-api.md) §3.
- Internal-only code lives under `internal/*` (Go's `internal/` convention enforces this).
- gRPC services proto-defined under `proto/<service>/v<N>/`; generated stubs under `pkg/.../pb/` via `ap generate`.
- eBPF (rare; OBI ships its own): `.bpf.c` files under `internal/bpf/`, bindings via `bpf2go`. Generated files are checked in.

## OBI integration boundary

Per [ADR-0018](docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library), OBI runs as a **sibling container** in the agent DaemonSet pod, not as an embedded Go library. The agent is an OTLP receiver that consumes from OBI on localhost.

Consequences for the codebase:

- **No package imports `go.opentelemetry.io/obi/*`.** The boundary is now "zero OBI Go imports anywhere," still enforced by the Go test in [`internal/archtest`](internal/archtest) (see [ADR-0016](docs/design/decisions.md#adr-0016-obi-import-boundary-enforced-via-go-test)).
- **OBI version pinning is image-tag based**, not `go.mod` based. The pin lives in `k8s/daemonset.yaml` and (once v1.0 lands) `helm/ollie/values.yaml`. Bump policy: one tag at a time, dedicated PR, contract tests green.
- **`pkg/capture` is the OBI-bridge package** (OTLP receiver + OBI config writer), not a Go-API wrapper. It exposes the same `Manager` interface from v0.1; the implementation talks OTLP and writes OBI's config file.

See [`docs/design/obi-integration.md`](docs/design/obi-integration.md) for the deployment topology, config flow, reload mechanism, and contract-test fixtures.

## Keeping this file current

This document is expected to drift if not actively maintained. **Edit it in the same PR as any change that affects conventions** — when new packages land, when build commands change, when the install namespace changes, when a milestone PR merges. Agentic coding tools have standing authorization to refresh it when they notice it's out of date.
