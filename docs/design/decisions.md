# Architecture Decisions

**Status:** Accepted, 2026-05-17
**Owners:** TBD

This is the canonical decision log for the project. Every cross-cutting decision is recorded here as an Architecture Decision Record (ADR). Design documents under [`docs/design/`](.) implement these decisions; if you find a conflict, this file wins and the design doc should be updated.

ADRs are append-only. If a decision is superseded, add a new ADR that supersedes the old one and update the old one's status — do not edit the old text.

> **Convention:** when a design doc cites an ADR, link by ID, e.g. `[ADR-0008](decisions.md#adr-0008-query-language)`. ADR IDs are stable and never reused.

> **Future split:** once this file exceeds ~15 ADRs we will split to `docs/design/decisions/0001-*.md` following the standard ADR-tools layout.

---

## ADR-0001: eBPF data plane = OpenTelemetry eBPF Instrumentation (OBI)

**Status:** Accepted, 2026-05-17

**Context.** The project requires transparent eBPF-based capture of L4 and L7 (HTTP/1.1, HTTP/2, gRPC, A2A) and TLS-decrypted L7. Three viable paths: (a) `opentelemetry-ebpf-instrumentation` (formerly Grafana Beyla, donated to OTel), (b) fork Pixie's PEM data plane, (c) build bespoke on `cilium/ebpf`. Requirements §6 fixed the eBPF approach but left the library choice for design.

**Decision.** Use OBI as the data plane (Go package `go.opentelemetry.io/obi/pkg/ebpf`). Wrap it behind a thin adapter in `pkg/capture` (see [ADR-0010](#adr-0010-obi-version-pinning-and-adapter)).

**Consequences.**
- ✅ OTLP-shaped output matches our pluggable-sink model with no translation layer.
- ✅ HTTP/1.1, HTTP/2, gRPC, SQL, and GenAI (OpenAI/Anthropic/Gemini) instrumentation is already shipped — the GenAI surface directly serves our "AI agents calling AI agents" use case.
- ✅ Kubernetes pod identity attachment is already implemented (Beyla-pattern); we inherit topology basics.
- ✅ Apache 2.0, vendor-neutral OTel governance, active development.
- ✅ `Tracer` interface allows custom protocol modules — A2A and (eventually) Kafka can be added without forking.
- ⚠️ OBI is v0.8 (April 2026) and promises breaking changes per minor — mitigated by [ADR-0010](#adr-0010-obi-version-pinning-and-adapter).
- ⚠️ TLS coverage is less deep than Pixie's; mitigated by upstream contribution policy in [ADR-0010](#adr-0010-obi-version-pinning-and-adapter).
- ❌ Kafka protocol parser not yet in OBI — deferred to roadmap.

**Rejected alternatives.**
- *Fork Pixie PEM.* Mature and battle-tested, but tightly coupled to Pixie's CMU column store and PxL execution model. Ripping out PEM's data plane to swap in our own would consume the very engineering capacity that's supposed to differentiate us from Pixie.
- *Bespoke on `cilium/ebpf`.* Rebuilding HTTP/2 + gRPC parsing + TLS uprobes is multiple person-years OBI has already done.

---

## ADR-0002: In-cluster store = Prometheus tsdb HEAD block + parallel ring buffer

**Status:** Accepted, 2026-05-17

**Context.** Each node needs a short-retention store sized for HPA's decision window and AI-agent recent-history queries. Required: bounded memory, sub-second filter+group-by, crash recovery. Options: flat files (the POC), embedded SQL/column store (DuckDB, ClickHouse-lite), Prometheus `tsdb` HEAD as a library, hand-roll.

**Decision.** Use Prometheus `tsdb` HEAD block (`github.com/prometheus/prometheus/tsdb`) as a library for **metrics**. Use a parallel typed in-memory append-only ring buffer for **spans and topology edges** that don't fit tsdb's series model. Both back themselves to disk per [ADR-0012](#adr-0012-tsdb-block-duration-and-wal-strategy).

**Consequences.**
- ✅ Thanos and Cortex prove tsdb HEAD is usable as a library — we are not the trailblazer.
- ✅ PromQL is the operationally-familiar query language for K8s metrics; HPAs already think in it.
- ✅ Apache 2.0, pure Go, no cgo.
- ✅ OTLP → Prometheus mapping is stable and well-defined.
- ⚠️ tsdb's data model is metrics-only; spans and edges need a parallel path. Acceptable; the ring buffer is small.
- ❌ We don't get SQL-style joins. We accept this — our queries are filter-then-group-then-aggregate, which PromQL covers.

**Rejected alternatives.**
- *DuckDB.* Excellent columnar engine, but embedding a C++ database in a per-node DaemonSet on COS adds cgo, binary size, and libc/kernel fragility we don't need.
- *Hand-roll a column buffer.* We would end up shaped like tsdb HEAD anyway, having reinvented WAL and label indexing.
- *VictoriaMetrics-style custom store.* Not usable as a library — it's a server, not a kit.

---

## ADR-0003: Onboarding model = hybrid CRD

**Status:** Accepted, 2026-05-17

**Context.** Requirements §2.2 required hybrid onboarding: a cluster-wide default plus per-workload opt-in/opt-out. CRDs are the K8s-idiomatic primitive and align with `PodMonitoring` (GKE Managed Prometheus) and `ServiceMonitor` (Prometheus Operator).

**Decision.** Two CRDs:
- `TrafficMonitor` — **namespaced**, selects workloads by labels, declares protocols/ports/cardinality knobs.
- `ClusterTrafficPolicy` — **cluster-scoped**, declares the default policy applied to pods not covered by any `TrafficMonitor`. Singleton recommended but not enforced; if multiple exist, the most-specific (by ordered priority field) wins.

**Consequences.**
- ✅ Aligns mental model with existing K8s monitoring CRDs.
- ✅ Cluster operators get one-CR coverage; workload teams get per-namespace overrides.
- ⚠️ Conflict resolution (multiple `TrafficMonitor`s selecting the same pod) requires care; see [`control-plane.md`](control-plane.md) for the resolution algorithm.

---

## ADR-0004: Library + controller posture; public API in `pkg/`

**Status:** Accepted, 2026-05-17

**Context.** Requirements §2.4 made "third parties can wrap us and register their own sinks" a load-bearing requirement, not a nice-to-have.

**Decision.** Ship as both a deployable controller binary and an importable Go library. Public API in `pkg/{capture,store,query,sink,topology,controller}`; everything else under `internal/`. Embedders import only what they need; the default binary registers all built-in sinks and CRD watchers.

**Consequences.**
- ✅ Third-party integrators get a clean import boundary — `pkg/` is supported; `internal/` is fair game to change.
- ✅ The default binary remains useful for non-embedders out of the box.
- ⚠️ Public API maintenance burden — see [`public-api.md`](public-api.md) for stability tiers.

---

## ADR-0005: Topology via Kubelet PID mapping + K8s informer

**Status:** Accepted, 2026-05-17

**Context.** Requirements §2.1 required source and peer K8s identity on every record. We need (a) PID → local pod, (b) IP → remote pod/service.

**Decision.** Local PID mapping comes from the node-local Kubelet `/pods` API plus `/proc/<pid>/cgroup` cross-reference, cached. Remote IP resolution comes from a K8s informer watching Pods + Services + EndpointSlices. Custody of that informer is [ADR-0009](#adr-0009-informer-custody--hybrid). Attribute namespace follows OTel semantic conventions: `k8s.pod.*`, `k8s.namespace.*`, `k8s.deployment.*`, `service.name`, mirrored as `peer.k8s.*` for the destination side.

**Consequences.**
- ✅ Inherits Beyla-pioneered pattern; OBI already provides much of the source-side attribution.
- ✅ Standard OTel semconv → off-the-shelf dashboards and downstream consumers Just Work.
- ⚠️ Informer-related API-server load — addressed by [ADR-0009](#adr-0009-informer-custody--hybrid).

---

## ADR-0006: Kernel/distro target = COS 125+, kernel 6.x, BTF/CO-RE required

**Status:** Accepted, 2026-05-17

**Context.** Per requirements §3. OBI itself requires Linux ≥ 5.8 with BTF, amd64 or arm64 (with a documented RHEL-family 4.18+ exception we are not using). cos-125 ships kernel 6.x.

**Decision.** Floor: Google Container-Optimized OS `cos-125-*`, kernel 6.x, BTF/CO-RE required, amd64 and arm64 only. GKE 1.35+ if any K8s API surface forces it. No legacy / non-BTF kernel paths.

**Consequences.**
- ✅ All modern eBPF features available (ring buffers, BTF, CO-RE, fentry/fexit, sleepable programs).
- ✅ No CO-RE relocation fallbacks; binary size and complexity stay small.
- ❌ Will not run on older distros. Documented constraint; not a regression.

---

## ADR-0007: License = Apache 2.0

**Status:** Accepted, 2026-05-17

**Context.** Requirements §3. Per [`.ap/headers.yaml`](../../.ap/headers.yaml) Apache 2.0 headers are auto-injected on Go and shell files.

**Decision.** Apache 2.0 for our code; every direct dependency must be Apache-2.0-compatible.

**Consequences.**
- ✅ Permissive, widely adopted, compatible with OBI, Prometheus tsdb, cilium/ebpf, k8s.io/client-go.
- ⚠️ GPL-only kernel headers in eBPF C are fine — eBPF programs typically declare `LICENSE = "Dual BSD/GPL"` in the `.bpf.c` itself, which is a kernel-verifier requirement not a license on our Go.

---

## ADR-0008: Query language

**Status:** Accepted, 2026-05-17

**Context.** Multiple consumers want different query semantics. HPA wants "give me a number." AI agents want filterable streams. Ops wants Prometheus-shaped scrape. Prior plans punted this as "PromQL + custom-for-spans (CEL?)" — the Plan-agent review correctly identified that as the single biggest under-decided item.

**Decision.**
- **PromQL** is the query language for **metrics** (tsdb-backed). Standard syntax, no extensions. Consumers: HPA's `custom.metrics.k8s.io` adapter, Prometheus scrape, ops dashboards.
- **CEL** is the query language for **spans, topology edges, and anything in the ring buffer** (non-tsdb data). CEL expressions are compiled against the OTLP proto types and evaluated per record. Consumers: AI agent streaming subscribers, `otelctl`-equivalent CLI, future UI.

The two languages serve disjoint data types; consumers pick the one matching what they want. The query server's gRPC API exposes both via separate methods (`QueryMetrics` taking PromQL, `QuerySpans` / `QueryEdges` taking CEL).

**Worked examples:**
- *HPA:* PromQL `avg(rate(ollie_http_requests_total{service="backend"}[1m]))` → custom-metrics-API adapter wraps the scalar result. See [`storage-and-query.md`](storage-and-query.md#hpa-example).
- *AI agent:* CEL `span.attributes["k8s.namespace.name"] == "payments" && span.duration_ms > 100` over the spans streaming endpoint. See [`storage-and-query.md`](storage-and-query.md#ai-agent-example).
- *Prometheus scrape:* `/metrics` endpoint exposes the tsdb directly; consumers issue PromQL against whatever scrapes us.

**Consequences.**
- ✅ Each consumer gets the language fit for its data; no awkward bridging.
- ✅ PromQL is operationally familiar; CEL is the standard for in-cluster policy filtering (admission webhooks, etc.).
- ⚠️ Two languages = more docs and examples to maintain. Accepted.
- ⚠️ Cross-data-type queries ("show me HTTP error rate AND the spans that produced them") require two calls. Acceptable for v1; revisit if pain emerges.

**Rejected alternatives.**
- *PromQL only.* Spans and edges aren't time series in the labeled-counter sense; jamming them into PromQL is hostile to span-shaped queries.
- *CEL only.* Loses PromQL's rate/avg/histogram functions and the HPA adapter would have to reimplement them.
- *Custom DSL.* Unjustified novelty. Both PromQL and CEL are off-the-shelf libraries.

---

## ADR-0009: Informer custody = hybrid

**Status:** Accepted, 2026-05-17

**Context.** Remote IP → pod/service resolution requires a K8s informer cache. Three options: (a) every agent runs its own informer (no central dependency, but N× API-server load on large clusters), (b) only the controller runs it and pushes to agents (one informer total, but controller becomes a critical-path dependency for capture), (c) hybrid — controller canonical, agent local fallback. Requirements §7.3 left this open.

**Decision.** Hybrid. The controller (leader-elected) runs the canonical informer for Pods, Services, and EndpointSlices. The controller broadcasts identity deltas to agents over the same gRPC stream used to distribute `MonitoringSpec` ([`control-plane.md`](control-plane.md)). Each agent maintains a local fallback informer that is **inactive** by default and activated only if the controller's heartbeat misses ≥3 intervals (default 15s). Once the controller is reachable again, the agent's fallback informer drops back to inactive after a hold-down period (60s) to avoid flapping.

**Consequences.**
- ✅ API-server load = 1 informer set in steady state.
- ✅ Controller is not a hard data-plane dependency — agents keep monitoring through controller outages.
- ⚠️ Agent has informer code it usually doesn't run. Memory cost is real but bounded.
- ⚠️ Brief transient mismatch on controller failover (agent activates local informer; one delta might be doubly-applied). Idempotent updates handle this.

**Rejected alternatives.**
- *Agents-only.* Scales linearly with node count; on 1000-node clusters that's 1000× the watch load.
- *Controller-only.* Controller failure stops identity resolution; new pods get unattributed flow records until recovery.

---

## ADR-0010: OBI version pinning and adapter

**Status:** Accepted, 2026-05-17

**Context.** OBI is v0.8 and explicitly says minor releases may break API and behavior. We depend on it for the deepest part of the stack. Direct dependency would make every OBI bump a project-wide refactor.

**Decision.** All OBI usage lives behind `pkg/capture`, a thin adapter that exposes:
- `type Tracer interface { ... }` — our trimmed surface (start/stop, AllowPID/BlockPID, callbacks for spans/metrics/events, protocol-module enable/disable).
- A `New(cfg Config) (Tracer, error)` constructor.
- No OBI types leak through the boundary; all are translated to our `pkg/capture` types or to OTel SDK types we already depend on.

Version policy:
- Pin exactly one OBI minor at a time in `go.mod`.
- Bumping OBI happens in a **dedicated PR** that touches `pkg/capture` only.
- A **contract-test suite** in `pkg/capture/contracttest/` replays recorded eBPF events and synthetic traffic against the adapter; bumps must pass the suite unchanged. New OBI features get new contract tests before being exposed.
- Fork-vs-upstream criteria for TLS coverage gaps: default to upstream contribution. Fork only if a critical hole sits unmerged for one full OBI release cycle.

**Consequences.**
- ✅ OBI churn is one-file blast radius, not project-wide.
- ✅ Contract tests catch behavioral regressions, not just type-shape changes.
- ⚠️ Adapter is an indirection; reviewers must keep it minimal or it becomes its own forking risk.
- ⚠️ We are bottlenecked on OBI's release cadence for new protocol modules. Mitigated by ability to register custom `Tracer`s.

---

## ADR-0011: Sink interface shape

**Status:** Accepted, 2026-05-17

**Context.** Sinks need to support push (sink-initiated writes to external systems), pull (external systems scrape us), and streaming (long-lived gRPC subscribers). One unified interface fits poorly; three interfaces are clearer.

**Decision.** Three explicit interfaces in `pkg/sink`:
- `PushSink` — `Write(ctx, batch) error`. Core calls into the sink on each write batch.
- `PullSink` — `RegisterRoutes(mux)`. Core gives the sink a chance to expose HTTP handlers that pull from the store on demand.
- `StreamingSink` — `Subscribe(ctx, filter) (<-chan Event, error)`. Long-lived; core feeds events into a channel until the consumer disconnects.

All three embed `Lifecycle { Init(ctx, deps) error; Start(ctx) error; Stop(ctx) error; Name() string }`. A single struct can implement multiple interfaces (e.g. the Prometheus sink implements both `PushSink` for remote-write and `PullSink` for the scrape endpoint).

Sinks are registered via `pkg/sink.Register(s Sink)` at process start; misbehaving sinks return errors that core counts and continues — a sink cannot crash the agent.

**Consequences.**
- ✅ Each pattern has the minimal, idiomatic interface.
- ✅ Embedders implement only what makes sense for their target.
- ⚠️ Three interfaces = three docs and three example sinks. Accepted.

**Rejected alternatives.**
- *Single `Sink` interface with mode flags.* Type system can't help; mistakes surface only at runtime.
- *Channel-based only (push-via-channel).* Doesn't fit pull-style consumers like Prometheus scrape.

---

## ADR-0012: tsdb block duration and WAL strategy

**Status:** Accepted, 2026-05-17

**Context.** Prometheus tsdb's block duration is normally 2 hours; that's wrong for our 10-minute retention budget. We also need crash recovery.

**Decision.**
- Block duration: **2 minutes**. Aligned to the snapshot cadence. Retention = 5 blocks by default (10 minutes total).
- WAL: enabled, in `/var/lib/ollie/wal/`, with periodic compaction every 30 seconds.
- Snapshot strategy: tsdb's native block compaction handles it. On crash, WAL replay restores the HEAD; older closed blocks survive on disk.

**Consequences.**
- ✅ 2-min blocks match our retention granularity; configurable for operators who want longer windows.
- ✅ WAL recovery is a tsdb-native code path; we don't reinvent.
- ⚠️ Smaller blocks = more files. Acceptable at our retention sizes.

---

## ADR-0013: Module layout = new `core/` AP root

**Status:** Accepted, 2026-05-17 — migration clause amended by [ADR-0014](#adr-0014-poc-removed-early-amends-adr-0013-migration-clause); layout clause superseded by [ADR-0015](#adr-0015-collapse-core-to-repo-root-supersedes-adr-0013-layout)

**Context.** Repo already has three AP roots (`/`, `opentelemetry/`, `obs/`) per [`AGENTS.md`](../../AGENTS.md). The fresh codebase needs a home. Adding to an existing root mixes new design with disposable POC.

**Decision.** Create a new AP root at `core/` with its own `.ap/`, `images/`, `k8s/`, and Go module `github.com/gke-labs/in-cluster-observability/core`. Public packages under `core/pkg/`; private under `core/internal/`. Default binary at `core/cmd/ollie/`. The existing POC roots (`/`, `opentelemetry/`, `obs/`) stay until the new code reaches parity, then get removed in a single cleanup PR.

**Consequences.**
- ✅ Clean separation between POC and production code during the build phase.
- ✅ Reuses the project's established AP-root convention.
- ✅ Module path makes the public API surface obvious to importers.
- ⚠️ Four AP roots in the repo temporarily. Mitigated by the cleanup-PR commitment.

---

## ADR-0014: POC removed early (amends ADR-0013 migration clause)

**Status:** Accepted, 2026-05-17 (amends [ADR-0013](#adr-0013-module-layout--new-core-ap-root))

**Context.** [ADR-0013](#adr-0013-module-layout--new-core-ap-root) anticipated a transitional period where the new `core/` AP root would coexist with the three POC roots (`/`, `opentelemetry/`, `obs/`) until parity, with a single cleanup PR at v1.0 GA (issue [#123](https://github.com/gke-labs/in-cluster-observability/issues/123)). Gari pushed back on this on 2026-05-17: carrying dead code through six milestones added noise without value — the design docs already capture everything the POC taught us, and the POC is preserved on `main` via git history.

**Decision.** Remove the POC AP roots from the `rewrite` branch immediately, before v0.1 implementation begins. Issue [#123](https://github.com/gke-labs/in-cluster-observability/issues/123) was closed early.

**Consequences.**
- ✅ The `rewrite` branch reflects what we're building, not what we abandoned.
- ✅ Less grep noise during implementation.
- ✅ No "is this the new code or the old?" ambiguity.
- ⚠️ Brief "no AP roots at all" state on the `rewrite` branch until v0.1 Foundation ([#64](https://github.com/gke-labs/in-cluster-observability/issues/64)) creates the new root. CI presubmits will either no-op or fail loudly until then. Accepted on a feature branch (not `main`).
- ⚠️ The e2e harness pattern from the POC's `tests/e2e/` is gone; needs to be re-introduced when v0.1 testing work begins. Source remains on `main` for reference (`git show main:tests/e2e/harness.go`).

**Supersedes.** ADR-0013's migration clause only (*"The existing POC roots (`/`, `opentelemetry/`, `obs/`) stay until the new code reaches parity, then get removed in a single cleanup PR"*). The structural decisions in ADR-0013 are otherwise unchanged.

**Implemented in.** Commit `e5235a9` on the `rewrite` branch.

---

## ADR-0015: Collapse `core/` to repo root (supersedes ADR-0013 layout)

**Status:** Accepted, 2026-05-17 (supersedes the layout clause of [ADR-0013](#adr-0013-module-layout--new-core-ap-root))

**Context.** [ADR-0013](#adr-0013-module-layout--new-core-ap-root) placed the new code under a `core/` AP root subdirectory. The rationale was to isolate the rewrite from the three POC AP roots that would coexist temporarily. With the POC removed early ([ADR-0014](#adr-0014-poc-removed-early-amends-adr-0013-migration-clause)) and only one AP root planned going forward, the original rationale no longer applies.

**Decision.** Put the new code at the **repo root**, not under `core/`:

- Single Go module `github.com/gke-labs/in-cluster-observability` (no `/core` suffix).
- Single AP root at the repo root: `/.ap/`.
- Public packages under `pkg/{capture,store,query,sink,topology,controller,schema,obsapi}`; private under `internal/`.
- Default binary at `cmd/ollie/`.
- Manifests at `k8s/`, images at `images/`, tests at `tests/`, protos at `proto/`, dashboards at `dashboards/`, Helm chart at `helm/`.

All path references in earlier ADRs and design docs are updated to drop the `core/` prefix. ADR-0013's original text is preserved as the historical decision record; this ADR documents the change.

**Consequences.**
- ✅ Idiomatic Go layout — packages at `pkg/capture` instead of `pkg/capture`.
- ✅ Module path matches the project name with no awkward suffix.
- ✅ Removes the only reason for `core/`, which was POC coexistence.
- ⚠️ Earlier design docs and the seed issues (~60) were authored with `core/` paths and were swept to drop the prefix.
- ⚠️ Loses optionality for future additional AP roots (e.g. a separate CLI repo or experimental subproject). Acceptable — if that becomes needed, we add a new AP root then.

**Supersedes.** ADR-0013's layout clause (`core/` subdirectory). ADR-0013's choices of `pkg/` vs `internal/`, public-API stability, and default-binary structure all stand.

**Implemented in.** Sed sweep across design docs, AGENTS.md, and seed issue bodies on 2026-05-17.

---

## ADR-0016: OBI import boundary enforced via Go test

**Status:** Accepted, 2026-05-17

**Context.** [ADR-0010](#adr-0010-obi-version-pinning-and-adapter) quarantines all OBI usage behind `pkg/capture` so OBI's v0 churn has one-file blast radius. That decision is only useful if the boundary is mechanically enforced — a code-review-only rule will be violated within a release. The design doc says "a linter / build rule fails if any other package imports go.opentelemetry.io/obi/* directly," but leaves the mechanism open.

**Decision.** Enforce the boundary as a **Go test** in `internal/archtest`. The test parses every `.go` file in the module (stdlib `go/parser`, imports-only) and fails if any file outside `pkg/capture/` imports a path under `go.opentelemetry.io/obi`. It runs as part of `go test ./...` and the CI `ap-test` presubmit; no separate tool or build-time hook is needed.

**Consequences.**
- ✅ No new dependencies (stdlib `go/parser` only).
- ✅ Fits the existing `go test` / `ap test` developer workflow; no new lint tool to install or wire into editors.
- ✅ Easy to extend with sibling architectural assertions (one Go file per invariant).
- ✅ Fast (low-hundreds-of-ms even at full repo size since we use `parser.ImportsOnly`).
- ⚠️ Runs at `go test` time, not at compile time. A developer who runs `go build` without `go test` can land a violation locally. CI catches it before merge.
- ⚠️ The test hardcodes `pkg/capture` as the only allowed importer. If a future ADR moves the adapter, this test must move with it.

**Rejected alternatives.**
- *Custom `go vet` analyzer.* More complex, needs separate distribution to developers; benefit is compile-time check, but CI catches this anyway.
- *Build tag / `//go:build` trick.* Doesn't compose well with the rest of the codebase; non-obvious failure mode.
- *Convention only, enforced by review.* Will be violated. The whole point of [ADR-0010](#adr-0010-obi-version-pinning-and-adapter) is mechanical isolation.

**Implemented in.** `internal/archtest/import_boundary_test.go` (commit landing v0.1).

---

## ADR-0017: v0.2 Capture MVP implementation decisions

**Status:** Accepted, 2026-05-17

**Context.** Five implementation-time decisions for v0.2 (Capture MVP, issues [#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77)) that aren't large enough to merit individual ADRs but should be captured before work begins. Filed as one ADR to keep the log tight; v0.2 implementation PRs reference this ADR for justification per sub-decision.

### 17.1 OBI version pin

**Decision.** v0.2's first OBI integration PR pins **the latest stable OBI v0.x at time of that commit** in `go.mod`. Per [ADR-0010](#adr-0010-obi-version-pinning-and-adapter), one minor at a time; subsequent bumps live in dedicated PRs with the contract-test suite green.

### 17.2 Self-observability metrics library = OpenTelemetry SDK

**Decision.** Use the **OpenTelemetry metrics SDK** (`go.opentelemetry.io/otel/metric` + `go.opentelemetry.io/otel/sdk/metric`) for self-observability across the agent, controller, and query server. The Prometheus scrape sink (v0.3 [#82](https://github.com/gke-labs/in-cluster-observability/issues/82)) wraps the SDK via `go.opentelemetry.io/otel/exporters/prometheus`, exposing `/metrics` in Prometheus text format with no functional loss for operators with existing Prometheus deployments.

**Rationale.**
- OBI emits OTel-shaped data; consistent metrics vocabulary across data plane and self-observability path. No "two metric SDKs to reason about."
- Single SDK powers OTLP push, Prometheus scrape, and any future OTel Collector receiver — one source of truth.
- Industry direction: OTel metrics SDK reached 1.0 stable; OBI / Beyla / Grafana ecosystem is OTel-native; Prometheus itself is converging toward OTLP ingestion. Picking `prometheus/client_golang` today reads as a legacy decision in two years.

**Rejected alternative.** `github.com/prometheus/client_golang` directly. Pros: smaller dep, ~5 lines to instantiate a counter. Cons: forks the project's metrics vocabulary, agent self-obs would be Prometheus-shaped while data-plane outputs are OTel-shaped, and any future OTLP push of self-obs metrics needs a translation shim.

**Consequences.** Three OTel modules become the first non-stdlib deps in `go.mod`. ~20–30 lines of boilerplate per component to instantiate `MeterProvider` / `Reader` / `Exporter`, paid once. Operators see no change at the wire — `/metrics` still serves Prometheus text format.

### 17.3 Debug HTTP endpoint = loopback-only, default off

**Decision.** The agent's debug HTTP endpoint ([#75](https://github.com/gke-labs/in-cluster-observability/issues/75)) binds **`127.0.0.1:9099`** only, behind a **`--debug-endpoint`** flag that **defaults to off**. No authentication required because the listener is loopback-only — access requires `kubectl exec` into the agent pod's network namespace.

**Rationale.** Avoids designing an auth story for a v0.2-only convenience surface. The endpoint exists to drive `AllowPID` / `BlockPID` manually until the controller (v0.4) takes over CRD-driven monitoring.

**Consequences.** Operators who want to drive the debug endpoint from another pod or node must wait for the controller. Acceptable for v0.2 — its audience is developers smoke-testing the capture path, not operators.

### 17.4 Strip OBI's built-in Kubernetes attribution

**Decision.** Disable OBI's native K8s identity attachment for v0.2 Events. All Kubernetes attribution lands via our own `pkg/topology` resolver starting in v0.3 ([#80](https://github.com/gke-labs/in-cluster-observability/issues/80), [#81](https://github.com/gke-labs/in-cluster-observability/issues/81)).

**Rationale.** Two sources of K8s metadata on the same Event creates "which is canonical?" ambiguity and forces our enricher to know what OBI already did to avoid double-decoration. [`docs/design/topology.md`](topology.md) assumes single ownership by `pkg/topology`; honoring that from v0.2 simplifies v0.3.

**Consequences.** v0.2 Events carry no `k8s.*` attributes (only PID + protocol + payload-specific fields per [17.5](#175-v02-metricspan-field-set--minimal-http-focused)). v0.3's enricher populates `k8s.*` from `pkg/topology`. Smoke tests in v0.2 show raw PID-tagged events; K8s-attributed events arrive with v0.3.

**Rejected alternative.** Let OBI attach its K8s attrs and have the enricher overwrite. Works but invites bugs when the two disagree.

### 17.5 v0.2 metric/span field set = minimal HTTP-focused

**Decision.** `MetricEvent` and `SpanEvent` in v0.2 carry only the fields needed to demo HTTP request count + duration via the debug log endpoint: `{path, method, status, duration_ns}` for HTTP; `{bytes_rx, bytes_tx, conns, rtt_ns}` for L4. Full OTel-shaped payloads (attributes maps, full semantic-convention coverage) arrive with v0.3 when there's a store to land in.

**Rationale.** Avoid designing the field set twice. v0.2 has no store and no enricher; sinking events to a debug log only requires the demo fields. v0.3 ([#83](https://github.com/gke-labs/in-cluster-observability/issues/83)) codifies the full schema via `pkg/schema`; the `MetricEvent` / `SpanEvent` types fill in then.

**Consequences.** `pkg/sink.Metric`, `Span`, `Edge` stay empty stubs through v0.2 (already true post-v0.1). Embedders in v0.2 should treat them as "shape only" — no real data flows. Path field is captured raw at this point (templating arrives in v0.6 [#108](https://github.com/gke-labs/in-cluster-observability/issues/108)); v0.2 cardinality is bounded only by the test workload's path set, which is acceptable for the milestone's local-test audience.

---

**Implemented in.** v0.2 milestone work ([#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77)). Each issue's PR references this ADR for the relevant sub-decision.

---

## Open and superseded ADRs

None yet. New ADRs are appended above this section.
