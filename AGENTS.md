# AGENTS.md

This file documents conventions and operational context for working in this repository. Both humans and agentic coding tools should read it before any non-trivial change. Keep it current as conventions evolve.

`GEMINI.md` is a stub that points back here so the two tools see the same content.

## Current state

This repo is in the middle of a planned rewrite. The legacy POC code (Prometheus + eBPF agent at the root, OpenTelemetry sink/query pipeline under `opentelemetry/`, the `obs/` logging library) was removed on the `rewrite` branch in commit `e5235a9`. POC code is preserved on `main` and reachable via `git log main -- <path>`.

All new code lands at the **repo root** as a single AP root and single Go module `github.com/gke-labs/in-cluster-observability` (see [ADR-0015](docs/design/decisions.md#adr-0015-collapse-core-to-repo-root-supersedes-adr-0013-layout)). As of this writing nothing has been built yet — see issue #64 (v0.1 Foundation) for the bootstrap.

What's in the repo right now:

- `docs/` — design (`docs/design/`), agreed requirements (`docs/requirements.md`), early rough sketch (`docs/rough_design.md`)
- `AGENTS.md` — this file
- `GEMINI.md` — stub pointing here
- `dev/ci/presubmits/` — CI wrappers (stale until the AP root is bootstrapped)
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

PR flow during the rewrite:

- **`rewrite`** is the integration branch. Direct pushes are allowed; no per-feature sub-branches.
- **Commit fine-grained.** One logically separable unit per commit. No WIP megacommits.
- **At each milestone boundary**, open a PR `rewrite` → `main`. One PR per milestone (v0.1, v0.2, …, v1.0). This is the review gate.
- **Never commit directly to `main`.** Main accumulates via milestone PRs.

## Build, test, lint (once the AP root is bootstrapped)

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
| Benchmark smoke | `dev/ci/presubmits/ap-bench-smoke` |

CI runs these via wrappers in `dev/ci/presubmits/`. If `ap build` fails in CI, **run it locally before claiming it passes**.

`ap-verify-generate` fails if `ap generate //...` produces a diff — the hint is the fix.

Until the AP root is bootstrapped (#64), all `ap` commands either no-op or fail with no projects found — expected on `rewrite`.

## Kubernetes manifest conventions (enforced by `ap`)

- Manifests live in a `k8s/` directory inside the AP root.
- **Do not set `imagePullPolicy`** unless there's a specific reason — `ap deploy` manages it.
- Image references should be the bare image name (e.g. `ollie`); `ap` adds the registry prefix at deploy.

## Apache 2.0 license headers

Every code/config artifact (Go, YAML, Dockerfile, proto, shell) carries the full Apache 2.0 license header with `Copyright 2026 Google LLC` at the top of the file. Auto-injected for Go and shell by `.ap/headers.yaml` once present; YAML, Dockerfile, and proto are added by hand. Markdown is unannotated by repo precedent.

The canonical header text is the one already in use by the repo; see `.ap/headers.yaml` (once it lands) for the file used by `ap generate`.

## Coding conventions

- Standard Go; minimize dependencies (prefer stdlib or established lightweight packages).
- Self-observability metric names are prefixed `ollie_<component>_*` — see [`docs/design/operations.md`](docs/design/operations.md) §5.
- Public Go API surface lives under `pkg/*` with explicit stability tags (`// Stability: Stable | Experimental | Internal`) — see [`docs/design/public-api.md`](docs/design/public-api.md) §3.
- Internal-only code lives under `internal/*` (Go's `internal/` convention enforces this).
- gRPC services proto-defined under `proto/<service>/v<N>/`; generated stubs under `pkg/.../pb/` via `ap generate`.
- eBPF (rare; OBI ships its own): `.bpf.c` files under `internal/bpf/`, bindings via `bpf2go`. Generated files are checked in.

## OBI integration boundary

The only file in the repo that may import `go.opentelemetry.io/obi/*` is the adapter in `pkg/capture`. Everything else depends on our `capture.Manager` interface. OBI is pinned to one minor at a time; bumps live in their own PR with the contract-test suite green. See [`docs/design/obi-integration.md`](docs/design/obi-integration.md) and [ADR-0010](docs/design/decisions.md#adr-0010-obi-version-pinning-and-adapter).

## Keeping this file current

This document is expected to drift if not actively maintained. **Edit it in the same PR as any change that affects conventions** — when the AP root lands, when new build commands appear, when the install namespace changes, when a milestone PR merges. Agentic coding tools have standing authorization to refresh it when they notice it's out of date.
