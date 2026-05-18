---
title: "Contributing"
linkTitle: "Contributing"
weight: 50
description: "How to file issues, structure PRs, and work on in-cluster-observability."
---

The canonical contributor reference is [`AGENTS.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/AGENTS.md) in the repo root — it covers conventions, build commands, layout, and the OBI integration boundary. This page is a short pointer.

## Filing issues

All issues and milestones live in upstream `gke-labs/in-cluster-observability`. Even if you're working on a fork, file issues against upstream — the fork's issue tracker is intentionally empty. `gh repo set-default` is configured in the upstream clone so `gh issue list` / `gh issue create` target upstream by default.

## Branch and PR model

One integration branch per milestone — `v0.1`, `v0.2`, …, `v1.0`. PRs **stack**: each milestone branch PRs to the previous (`v0.3 → v0.2 → v0.1 → main`). When a milestone PR merges to `main`, GitHub auto-updates the next PR's base.

Within a milestone branch, commits are **fine-grained** — one logically separable unit per commit. No WIP megacommits. Hygiene fixups go onto the active milestone branch as small extra commits, not into `main` and not onto a later milestone branch.

## Build and test

The project uses [`ap`](https://github.com/gke-labs/gke-labs-infra/tree/main/ap) (autoproject):

```sh
go run github.com/gke-labs/gke-labs-infra/ap@latest test //...
go run github.com/gke-labs/gke-labs-infra/ap@latest lint //...
go run github.com/gke-labs/gke-labs-infra/ap@latest build //...
```

For quick local iteration, plain Go commands work too:

```sh
go build ./...
go test ./...
```

CI runs `ap` via wrappers in `dev/ci/presubmits/`. If `ap build` fails in CI, **reproduce it locally before claiming it passes**.

## Apache 2.0 license header

Every code/config artifact (Go, YAML, Dockerfile, proto, shell) carries the full Apache 2.0 license header with `Copyright 2026 Google LLC`. Auto-injected for Go and shell; YAML, Dockerfile, and proto get it by hand. Markdown is unannotated by repo precedent. `go.mod`, `go.sum`, `LICENSE`, and `.git*` files are skipped.

## OBI integration boundary

Per [ADR-0018](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library), OBI runs as a **sibling container**, not an embedded Go library. The boundary is enforced by a Go test in `internal/archtest`: **no package imports `go.opentelemetry.io/obi/*`**. OBI version pinning is image-tag-based, lives in `k8s/daemonset.yaml`, and is bumped one tag at a time in dedicated PRs with the contract tests green.

## See also

- [`AGENTS.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/AGENTS.md) — the full conventions doc.
- [`docs/design/decisions.md`](https://github.com/gke-labs/in-cluster-observability/blob/main/docs/design/decisions.md) — every architectural decision recorded as an ADR.
- [Upstream issues](https://github.com/gke-labs/in-cluster-observability/issues) — file bugs, request features, browse milestones.
