# AGENTS.md

This file documents conventions and operational context for working in this repository. Both humans and agentic coding tools should read it before any non-trivial change. Keep it current as conventions evolve.

`GEMINI.md` is a stub that points back here so the two tools see the same content.

## Current state

This repo is in the middle of a planned rewrite. The legacy POC code (Prometheus + eBPF agent at the root, OpenTelemetry sink/query pipeline under `opentelemetry/`, the `obs/` logging library) was removed early in the rewrite. POC code is preserved on `main` and reachable via `git log main -- <path>`.

All code lives at the **repo root** as a single AP root and single Go module `github.com/gke-labs/in-cluster-observability` (per [ADR-0015](docs/design/decisions.md#adr-0015-collapse-core-to-repo-root-supersedes-adr-0013-layout)). The **v0.1 Foundation** milestone ([#64](https://github.com/gke-labs/in-cluster-observability/issues/64)–[#69](https://github.com/gke-labs/in-cluster-observability/issues/69)) has landed: the AP root is bootstrapped, the public package skeletons exist with no-op implementations, the OBI adapter shell is in place, and the container image + DaemonSet manifest deploy as a no-op pod. v0.2 (Capture MVP) wires actual eBPF and the first real protocol modules.

What's in the repo:

- `docs/` — design (`docs/design/`), agreed requirements (`docs/requirements.md`), early rough sketch (`docs/rough_design.md`)
- `AGENTS.md` — this file
- `GEMINI.md` — stub pointing here
- `.ap/` — autoproject config (`ap.yaml`, `headers.yaml`)
- `go.mod` — single Go module
- `cmd/ollie/` — default binary (stub in v0.1; prints version and exits unless `--stay-alive`)
- `pkg/` — public API: `capture`, `obsapi`, `sink`, `topology`, `store`, `query`, `controller`, `schema`
- `internal/` — private code; `internal/archtest/` enforces the OBI import boundary
- `images/ollie/` — Dockerfile (distroless static, CGO disabled)
- `k8s/` — install manifests (namespace + DaemonSet + kustomization)
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

The only package in the repo that may import `go.opentelemetry.io/obi/*` is `pkg/capture`. Everything else depends on the `capture.Manager` interface. The boundary is enforced by a Go test in [`internal/archtest`](internal/archtest) that parses every `.go` file in the module and fails if any path outside `pkg/capture` imports OBI directly — see [ADR-0010](docs/design/decisions.md#adr-0010-obi-version-pinning-and-adapter) and [ADR-0016](docs/design/decisions.md#adr-0016-obi-import-boundary-enforced-via-go-test).

OBI is pinned to one minor at a time; bumps live in their own PR with the contract-test suite green. See [`docs/design/obi-integration.md`](docs/design/obi-integration.md).

## Keeping this file current

This document is expected to drift if not actively maintained. **Edit it in the same PR as any change that affects conventions** — when new packages land, when build commands change, when the install namespace changes, when a milestone PR merges. Agentic coding tools have standing authorization to refresh it when they notice it's out of date.
