# v0.1 Foundation — Hand-off and Smoke Tests

Status as of the seven v0.1 commits on top of `fd13b28` on `upstream/rewrite`. The full branch (v0.1 plus prior doc work) has been rewritten to use the `ollie` name and force-pushed; commit hashes below are post-rewrite.

## What landed

| Commit | Issue(s) | Summary |
|---|---|---|
| `7bfc5a6` | Closes #69, refs #64 | `.ap/ap.yaml` + `.ap/headers.yaml` (Apache 2.0 + Google LLC; skip list for YAML/MD/go.mod/etc.) |
| `65fbb04` | Closes #64 | `go.mod` (`github.com/gke-labs/in-cluster-observability`, Go 1.25) + `cmd/ollie/main.go` stub + test |
| `7e75ace` | Refs #65 | Public package skeletons: `pkg/{sink,topology,store,query,controller,schema}` with Stability tags |
| `b3bd6d8` | Closes #65, #66 | `pkg/capture` OBI adapter shell (no-op `Manager` + full type surface per `obi-integration.md` §2.1) + `pkg/obsapi` facade + `internal/archtest` import-boundary test |
| `ed73df2` | Closes #68 | `images/ollie/Dockerfile` (distroless static, CGO off) + `k8s/{namespace,daemonset,kustomization}.yaml` + `--stay-alive` flag on the binary |
| `f6a4fb3` | Closes #67 | Removes the dead `ap-e2e-opentelemetry` presubmit + workflow job |
| `0acc0c5` | (post-v0.1 hygiene) | Refreshes `AGENTS.md` for the post-v0.1 state; adds **ADR-0016** documenting the OBI boundary enforcement mechanism |

Use `git log --oneline fd13b28..HEAD` to see them all (7 v0.1 commits on top of the pre-v0.1 doc work).

> **Note on the repo vs. project name.** The Go module path is `github.com/gke-labs/in-cluster-observability` — this is the *repo* name, unchanged. The *project / binary / image / namespace / metric prefix* is `ollie`. Renaming the repo itself is a separate step the user has deferred.

## New design decision recorded

**[ADR-0016](docs/design/decisions.md#adr-0016-obi-import-boundary-enforced-via-go-test)** — OBI import boundary is enforced as a Go test in `internal/archtest`, not a custom linter. This was an implementation-time decision that fell out of #66 ("a linter / build rule fails if any other package imports OBI directly"). Stdlib `go/parser` only, runs as part of `go test ./...` and `ap-test`.

Two implementation choices that did **not** get an ADR (judged not architecturally significant; recorded here for the v0.1 PR description):

- The v0.1 `ollie` binary defaults to "print version and exit." Pass `--stay-alive` for daemon deployments — the DaemonSet manifest does. Real lifecycle wiring lands with v0.2.
- The v0.1 DaemonSet runs **unprivileged** — no `CAP_BPF`, no `hostPID`, no host mounts, runs as uid 65532. The agent does nothing kernel-side yet. Privilege escalation arrives with the actual capture work in v0.2 ([#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#73](https://github.com/gke-labs/in-cluster-observability/issues/73)).

## Smoke tests

### 1. Local build + tests (fast, no Docker / Kind required)

```sh
go build ./...
go test ./...
```

Expected: every package compiles; tests pass. The `internal/archtest` test exercises the OBI import boundary — currently passes vacuously (no package imports OBI yet).

### 2. Run the binary

```sh
go run ./cmd/ollie --version
# v0.1.0-dev

go run ./cmd/ollie
# ollie v0.1.0-dev
# v0.1 Foundation: stub binary, no functionality wired yet.
# Pass --version to print only the version string.

# Long-lived variant (the daemon mode):
go run ./cmd/ollie --stay-alive
# (blocks; ctrl-C to exit)
```

### 3. Lint with `ap`

```sh
go run github.com/gke-labs/gke-labs-infra/ap@latest lint //...
go run github.com/gke-labs/gke-labs-infra/ap@latest test //...
```

Network access required (downloads `ap` from the module proxy on first run). Should succeed; nothing to lint flags should fire.

### 4. Verify generated state is clean

```sh
go run github.com/gke-labs/gke-labs-infra/ap@latest generate //...
git diff --exit-code
```

`ap generate` may inject license headers it thinks are missing. If `git diff` is non-empty after generate, something was authored without a header — commit the fix.

### 5. Build the image (Docker required)

```sh
docker build -t ollie:v0.1 -f images/ollie/Dockerfile .
docker run --rm ollie:v0.1 --version
# v0.1.0-dev
```

The image is distroless static. Final binary is ~3–5 MB stripped.

### 6. Deploy to Kind (full path)

```sh
# Spin up Kind, load the image, deploy.
kind create cluster --name ollie-v01
kind load docker-image --name ollie-v01 ollie:v0.1
kubectl apply -k k8s/

# The manifest references the bare image name "ollie". Patch
# the DaemonSet's image to match what you loaded:
kubectl set image -n ollie-system daemonset/ollie-agent agent=ollie:v0.1

# Wait for the DaemonSet to reach Ready.
kubectl rollout status -n ollie-system daemonset/ollie-agent --timeout=60s

# Look at the pod log — should show the stub's version banner and the
# "blocking on SIGINT/SIGTERM" line.
kubectl logs -n ollie-system -l app.kubernetes.io/component=agent --tail=20
```

Expected: pod reaches `Running`, DaemonSet rolls out, log shows the version + stay-alive lines. **The agent does nothing else** — by design for v0.1.

Tear down:

```sh
kind delete cluster --name ollie-v01
```

### 7. ADR / archtest spot check

```sh
# Confirm the boundary test exists and runs.
go test -v ./internal/archtest -run TestNoOBIImportsOutsideCapture
```

To prove it actually catches violations, temporarily add `import _ "go.opentelemetry.io/obi/pkg/ebpf"` to any file under `pkg/obsapi` (or wherever non-capture). Re-run the test — should fail with a clear violation line. Don't commit that.

## What is *not* in v0.1

These are the v0.2+ deliverables, called out so smoke testing doesn't expect them:

- No real eBPF capture (v0.2 — [#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77))
- No `/metrics` endpoint (v0.3 — Prometheus scrape sink)
- No CRDs, no controller, no validating webhook (v0.4)
- No query server, no custom-metrics API, no HPA path (v0.5)
- No RBAC (kept minimal until the agent actually needs cluster API access)

## Suggested PR shape for the v0.1 milestone

When opening `rewrite` → `main` for v0.1:

- **Title:** `v0.1 Foundation`
- **Body:** the table above (commits + issues closed) plus the SMOKE_TEST.md instructions inline (or as a linked artifact)
- **Closes:** #64, #65, #66, #67, #68, #69 (issue #69 closed by the first commit's `Closes #69` trailer; others by their respective commits)

## Files left in the working tree

This `SMOKE_TEST.md` itself is the only untracked file. Move, commit, or discard as you prefer — it's not on any milestone's deliverable list.
