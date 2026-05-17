# v0.2 Capture MVP — Hand-off and Smoke Tests

Status as of `e8ef146` on `v0.2`. Nothing has been pushed.

## What landed

v0.2 implements the sibling-container model from [ADR-0018](docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library). The agent runs OTLP receivers on loopback + writes OBI's config; OBI runs in a sibling container that does the eBPF capture. Implementation-time decisions are documented in [ADR-0019](docs/design/decisions.md#adr-0019-v02-capture-mvp-implementation-time-decisions).

| Commit | Issue | Summary |
|---|---|---|
| `9430f7b` | Closes #76 | OTel metrics SDK + `pkg/capture/metrics.go`; `Metrics()` returns a real handle with `ollie_capture_*` counters |
| `4ee64ef` | (foundation) | `internal/obiconfig`: typed OBI config schema + atomic YAML writer |
| `ed3ee4a` | (foundation) | `internal/otlpreceiver`: loopback-only OTLP gRPC + HTTP receivers |
| `f43a99b` | Closes #70 | `pkg/capture.NewBridge`: real Manager wiring receiver + config writer |
| `8ae50fa` | Closes #71 | AllowPID/BlockPID → OBI discovery + 500ms reload coalescer |
| `85b5088` | Closes #72 | L4 TCP OTLP metrics → `capture.Event{Kind:Metric}` |
| `3282e57` | Closes #73 | HTTP/1.1 OTLP traces → `capture.Event{Kind:Span}` |
| `3bd9227` | (follow-up #73) | Wired `OnTraces` to `TranslateTraces` |
| `9143598` | Closes #77 | Panic recovery + `ModuleDegraded` event + OBI restart reporter |
| `d5d2095` | Closes #75 | `internal/debugendpoint`: loopback PID-control HTTP endpoint |
| `cc624c1` | Closes #74 | Contract-test harness + first synthetic fixtures (`l4-basic`, `http1-basic`) |
| `b7effa8` | (operator) | DaemonSet gains `obi` sibling container; `cmd/ollie` main.go wires all the above |
| `25ad3e5` | (docs) | AGENTS.md refresh for v0.2 |
| `e8ef146` | (decisions) | ADR-0019 — v0.2 implementation-time decisions |

`git log --oneline v0.1..HEAD` shows all 18 commits (the prior four are the ADR-0018 pivot work that landed before implementation).

## New design decisions (recorded in `docs/design/decisions.md`)

- **[ADR-0018](docs/design/decisions.md#adr-0018-obi-as-sibling-container-not-embedded-library)** — OBI as sibling container, not embedded library (the architectural pivot).
- **[ADR-0019](docs/design/decisions.md#adr-0019-v02-capture-mvp-implementation-time-decisions)** — Eight v0.2 implementation-time sub-decisions (OTLP receiver = direct gRPC, YAML lib = yaml.v3, reload = file-watch, debounce = 500ms, OBI image pin = v0.9.0, fixtures = synthetic seed first, translators exported, stale-config detection deferred to v0.3).

## Smoke tests

### 1. Local build + unit tests

```sh
go build ./...
go test ./...
```

Expected:
- Every package compiles.
- Tests pass for `cmd/ollie`, `pkg/capture`, `pkg/obsapi`, `pkg/sink`, `internal/archtest`, `internal/debugendpoint`, `internal/obiconfig`, `internal/otlpreceiver`, `tests/contract/obi`.

### 2. Run the agent locally (no OBI)

```sh
go run ./cmd/ollie \
  --otlp-grpc-addr=127.0.0.1:4317 \
  --otlp-http-addr=127.0.0.1:4318 \
  --obi-config=/tmp/obi-config.yaml \
  --debug-endpoint
```

Expected stderr:
```
ollie v0.2.0-dev
v0.2 Capture MVP: starting OTLP receiver + OBI config writer (per ADR-0018)
OTLP receiver: gRPC=127.0.0.1:4317 HTTP=127.0.0.1:4318; OBI config: /tmp/obi-config.yaml
debug endpoint enabled on 127.0.0.1:9099 (loopback)
```

Look at the generated `/tmp/obi-config.yaml`:
```sh
cat /tmp/obi-config.yaml
```
Should contain the loopback OTLP endpoints, `kubernetes.enable: false` (per ADR-0017.4), and an empty `discovery.services` list.

### 3. Drive AllowPID via the debug endpoint

In another shell while the agent is running:
```sh
curl -sS -X POST http://127.0.0.1:9099/debug/allow-pid \
     -d '{"pid": 12345, "modules": [2]}'   # 2 = ModuleHTTP1
# (no output; 204 No Content)

curl -sS http://127.0.0.1:9099/debug/state
# {"pids":null,"modules":[]}
```

Wait ~600ms then re-check the OBI config:
```sh
cat /tmp/obi-config.yaml
```
Now contains a `services:` entry named `pid-12345`. Verifies the coalesced writer.

Block the PID:
```sh
curl -sS -X POST http://127.0.0.1:9099/debug/block-pid -d '{"pid": 12345}'
```
After ~600ms, the entry is gone from the YAML.

### 4. Send synthetic OTLP to the receiver

While the agent is running, push an OTLP metric via gRPC (using `grpcurl` or any OTLP client). Example with the OTel collector's `telemetrygen`:
```sh
go install github.com/open-telemetry/opentelemetry-collector-contrib/cmd/telemetrygen@latest
telemetrygen metrics --otlp-endpoint 127.0.0.1:4317 --otlp-insecure --rate 5 --duration 5s
```

The agent's `ollie_capture_events_total` counter ticks for each received metric (visible via OTel metrics SDK exporter once v0.3's Prometheus scrape sink lands — for v0.2 the counters are in-memory only).

### 5. Run the contract tests

```sh
go test ./tests/contract/obi -v
```

Two cases pass: `l4-basic`, `http1-basic`. To regenerate goldens after a translator change:
```sh
go test ./tests/contract/obi -update
```

To bootstrap or rewrite the synthetic fixtures themselves:
```sh
go test ./tests/contract/obi -seed -update
```

Full recipe (including how to replace synthetic seeds with real OBI recordings) in [`tests/contract/obi/REGENERATE.md`](tests/contract/obi/REGENERATE.md).

### 6. Build the image

```sh
docker build -t ollie:v0.2 -f images/ollie/Dockerfile .
docker run --rm ollie:v0.2 --version
# v0.2.0-dev
```

### 7. Deploy to Kind with real OBI

This is the full end-to-end smoke test. Requires Docker + Kind.

```sh
# 1. Spin up a Kind cluster.
kind create cluster --name ollie-v02

# 2. Build + load the agent image.
docker build -t ollie:v0.2 -f images/ollie/Dockerfile .
kind load docker-image --name ollie-v02 ollie:v0.2

# 3. Apply the manifest. The DaemonSet has TWO containers now:
#    - obi    (ghcr.io/open-telemetry/obi:v0.9.0 — pulled from upstream)
#    - agent  (ollie:v0.2 — the image we just loaded)
kubectl apply -k k8s/
kubectl set image -n ollie-system daemonset/ollie-agent agent=ollie:v0.2

# 4. Wait for the DaemonSet to reach Ready (both containers).
kubectl rollout status -n ollie-system daemonset/ollie-agent --timeout=120s

# 5. Inspect each container's logs.
POD=$(kubectl get -n ollie-system pod -l app.kubernetes.io/component=agent -o jsonpath='{.items[0].metadata.name}')
kubectl logs -n ollie-system "$POD" -c agent --tail=10
kubectl logs -n ollie-system "$POD" -c obi   --tail=10

# 6. Exec into the agent container and check the shared OBI config.
kubectl exec -n ollie-system "$POD" -c agent -- cat /etc/ollie/obi-config/config.yaml

# 7. Deploy a tiny test workload and let OBI auto-discover it (real OBI
#    discovery is config-driven; v0.2 lets us drive PID selection via
#    the debug endpoint, but it's disabled by default in the manifest).
#    For a full e2e with real PID selection, run the agent with
#    --debug-endpoint set (patch the DaemonSet args) then exec in and
#    POST /debug/allow-pid for an httpbin pod's PID.
```

Tear down:
```sh
kind delete cluster --name ollie-v02
```

### 8. Verify the OBI import boundary

```sh
go test -v ./internal/archtest -run TestNoOBIImportsOutsideCapture
```

Should pass — under ADR-0018 NO package imports `go.opentelemetry.io/obi/*` (the test's contract widened from "only `pkg/capture` may import" to "nobody may import").

## What is *not* in v0.2

These belong to later milestones and intentionally do not work yet:

- **No K8s identity attribution** on events — `pkg/topology` PID cache and informer-driven peer resolution land in v0.3 (issues [#80](https://github.com/gke-labs/in-cluster-observability/issues/80) / [#81](https://github.com/gke-labs/in-cluster-observability/issues/81)).
- **No store** — events are received, translated, and silently consumed; v0.3 wires the Prometheus `tsdb` HEAD + ring buffer (#78, #79, #84).
- **No `/metrics` Prometheus surface** — self-obs counters tick in-memory only until v0.3's promscrape sink (#82) wraps the OTel SDK via `otel/exporters/prometheus`.
- **No CRDs, controller, webhook** — v0.4 work.
- **No HPA path / query server** — v0.5 work.
- **No HTTP/2, gRPC, TLS uprobes** — v0.6 protocol-hardening work.

## Suggested PR shape for the v0.2 milestone

When opening `v0.2` → `v0.1` (or `main` if v0.1's PR has merged by then):

- **Title:** `v0.2 Capture MVP`
- **Body:** the commit table above plus the smoke-test recipes 1–8.
- **Closes:** #70, #71, #72, #73, #74, #75, #76, #77.
- **Merge strategy:** create merge commit (preserves the fine-grained history; per AGENTS.md branch workflow).

## Open items for v0.3 to address

- Wire the `pkg/topology` enricher to populate `k8s.*` attributes on events (stripped from OBI at translation time per ADR-0017.4).
- Wire the `pkg/store` tsdb HEAD + ring buffer for events.
- Wire the Prometheus scrape sink (`pkg/sink/promscrape`) to expose `/metrics` via `otel/exporters/prometheus`.
- Add OBI-container-status watcher (vs. the current pull-based `ReportOBIRestart` exposed for operators).
- Add stale-config-rejection detection (per ADR-0019.8).
- Replace synthetic seed contract fixtures with real-OBI recordings (per `tests/contract/obi/REGENERATE.md`).

## Files left in the working tree

This `SMOKE_TEST_v0.2.md` is the only untracked file. The v0.1 `SMOKE_TEST.md` from the previous milestone is still present too (also untracked). Move, commit, or discard either as you prefer — neither is on a milestone's deliverable list.
