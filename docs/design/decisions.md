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

**Status:** Accepted, 2026-05-17 — adapter-shape clause superseded by [ADR-0018](#adr-0018-obi-as-sibling-container-not-embedded-library); version-pinning principle stands but now applies to the OBI container image tag, not the Go module

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

**Status:** Accepted, 2026-05-17 — sub-decisions 17.1, 17.4, 17.5 amended by [ADR-0018](#adr-0018-obi-as-sibling-container-not-embedded-library) (OBI runs as a sibling container, not an embedded Go library); 17.2 and 17.3 stand

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

## ADR-0018: OBI as sibling container, not embedded library

**Status:** Accepted, 2026-05-17 — supersedes [ADR-0010](#adr-0010-obi-version-pinning-and-adapter)'s adapter-shape clause; amends [ADR-0017](#adr-0017-v02-capture-mvp-implementation-decisions) sub-decisions 17.1, 17.4, 17.5

**Context.** ADR-0010 assumed OBI's `pkg/ebpf` was suitable for embedded library use, anticipating a thin adapter (~600–800 LoC) wrapping its Go API. Probing OBI v0.9.0 at the start of v0.2 implementation revealed reality:

- `pkg/ebpf.NewProcessTracer(tracerType, []Tracer, *obi.Config, imetrics.Reporter) *ProcessTracer` is a low-level building-block API. Embedders supply protocol-module `Tracer` implementations themselves (each `Tracer` is a ~15-method interface mixing `PIDsAccounter`, `KprobesTracer`, `GoProbes`, `UProbes`, `SocketFilters`, `SockMsgs`, `SockOps`, `Iters`, `Tracing`, instrumented-lib bookkeeping, offset registration, and event-context wiring) plus an `EBPFEventContext`, an `*obi.Config`, and a `msg.Queue[[]request.Span]` for output.
- OBI's higher-level orchestrator (`pkg/appolly/instrumenter.go`) is undocumented and uses OBI-internal types.
- OBI's README and supported deployment model is **as a standalone binary that emits OTLP**, not as a Go library to compose with.
- The actual lift to wrap `pkg/ebpf` is multiple person-weeks per protocol with the adapter pinned to OBI's internal contracts — exactly the churn ADR-0010 sought to insulate against.

**Decision.** Run OBI as a **sibling container** in the same agent DaemonSet pod. OBI emits OTLP to `127.0.0.1:4317`; our agent is an **OTLP receiver** that runs the enrichment, store, and sinks. The agent does **not** import OBI as a Go dependency.

Deployment shape:

| Container | Image | Privileges | Purpose |
|---|---|---|---|
| `obi` | `ghcr.io/open-telemetry/obi:<pinned-tag>` | `CAP_BPF` + `CAP_PERFMON` + `CAP_NET_ADMIN` + `hostPID` + host mounts | eBPF capture; emits OTLP to localhost |
| `agent` | `ollie:<our-tag>` | **unprivileged**; `runAsNonRoot: true` | OTLP receiver → enricher → store → sinks; OBI config writer |

Both containers share the pod network namespace (loopback for OTLP) and the pod lifecycle. OBI's config is mounted from a ConfigMap that the controller (v0.4) writes; on `MonitoringSpec` changes the controller updates the ConfigMap and either signals reload or relies on OBI's config watch.

The `pkg/capture.Manager` interface from v0.1 stays — its implementation pivots from "Go API caller" to "OTLP receiver + OBI lifecycle controller (config writer + reload signaler)."

**Consequences.**

- ✅ Adapter complexity drops dramatically. No `Tracer` interface implementations; no wiring through OBI's internal API surface; no `EBPFEventContext` plumbing.
- ✅ OBI version churn is decoupled from our build. Image-tag bump = test in CI, ship. No `pkg/capture` rewrites per OBI minor.
- ✅ Security posture improves: only the OBI container runs privileged. The agent container has no `CAP_BPF`, no `hostPID`, no host mounts. The threat model in [`operations.md` §4](operations.md) simplifies meaningfully.
- ✅ Aligns with how OBI is designed and supported by upstream. We consume the tool the way its maintainers intended.
- ✅ Matches the broader OTel ecosystem pattern (Beyla-as-sidecar, OTel Collector composition, Jaeger agent).
- ⚠️ Two images in the agent pod. Resource overhead is two processes instead of one; OBI's footprint is what it always was.
- ⚠️ Custom protocol modules (e.g. A2A semantic layer, Kafka before OBI supports it) become **upstream contributions** rather than additions to our adapter. Less control, but better fit for the ecosystem and easier to maintain.
- ⚠️ Per-PID enable/disable becomes **config-driven** (rewrite OBI's discovery config + signal reload), not direct API call. Slightly less crisp but operationally fine and matches OBI's own model.

**Supersedes.** ADR-0010's "thin adapter wrapping OBI's library API" framing. ADR-0010's "one version pinned at a time, dedicated bump PRs with contract tests green" principle stands but applies to the OBI **image tag**, not the Go module.

**Amends ADR-0017.**

- **17.1 OBI version pin:** now pin the OBI **image tag** in our manifest/Helm values, not the Go module in `go.mod`. The Go module dep was probe-only and is removed.
- **17.4 Strip OBI's K8s attribution:** now via OBI **configuration** (disable Kubernetes metadata decoration in OBI's config), not in our adapter.
- **17.5 Minimal field set:** OBI emits OTLP. Our v0.2 work is OTLP→`capture.Event` translation — much simpler than the originally anticipated OBI-event→`Event` translation.

**Sub-decisions 17.2 (OTel metrics SDK) and 17.3 (debug HTTP endpoint loopback-only) stand unchanged.**

**Rejected alternatives.**

- *Deep embed via `pkg/ebpf` directly.* Multi-week per protocol, fragile against OBI minor versions, no documented support path. Already costly at v0.2; intractable across v0.6's TLS coverage and v1.0's full protocol suite.
- *Vendor OBI's internal pipeline (`pkg/appolly/instrumenter.go`).* Undocumented and OBI-internal. Would force us to track every internal change with no upstream guarantees.
- *Switch eBPF library.* Forking Pixie PEM hits the same embed-vs-sibling dilemma with worse coupling. Bespoke `cilium/ebpf` is a multi-year reinvention of what OBI already does. No clearly better alternative for our requirements.

**Implementation impact on v0.2.** Issues [#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77) stay valid but their implementations pivot:

- **#70 Manager Start/Stop/EnableModule:** Start launches an OTLP receiver on `127.0.0.1:4317` (gRPC) and `:4318` (HTTP); module toggles update the OBI config and signal reload.
- **#71 AllowPID/BlockPID:** writes per-PID enable/disable into OBI's discovery config (a YAML file mounted from a ConfigMap).
- **#72 L4 TCP translation, #73 HTTP/1.1 translation:** translate OTLP `ExportMetricsServiceRequest` / `ExportTraceServiceRequest` to `capture.Event`. Field-set commitment from 17.5 stands.
- **#74 Contract tests:** fixtures are now recorded OTLP request bodies (binary protobuf), not eBPF event recordings. Simpler to capture; can be generated by replaying canary traffic through a real OBI.
- **#75 Debug HTTP endpoint:** unchanged.
- **#76 Self-obs metrics:** unchanged.
- **#77 Panic recovery:** the agent-side panic recovery shrinks (no eBPF reader goroutine to wrap). What's added is *OBI container health monitoring* — if the OBI sidecar dies repeatedly, our agent emits `incluster_obs_capture_obi_restarts_total` and surfaces the issue. Container restart itself is k8s's job.

**Implementation status.** Approved for v0.2 implementation on 2026-05-17. v0.2 work proceeds under the sibling-container model as documented above; the design is settled and not subject to re-litigation absent new information. Implementation is tracked across milestone v0.2 issues [#70](https://github.com/gke-labs/in-cluster-observability/issues/70)–[#77](https://github.com/gke-labs/in-cluster-observability/issues/77), whose bodies were updated on the same date to reflect this model. The first v0.2 implementation PR will include the manifest update adding the OBI container to the agent DaemonSet.

---

## ADR-0019: v0.2 Capture MVP implementation-time decisions

**Status:** Accepted, 2026-05-17

**Context.** v0.2 implementation under the sibling-container model ([ADR-0018](#adr-0018-obi-as-sibling-container-not-embedded-library)) surfaced several small choices that aren't large enough to warrant individual ADRs but should be recorded for future readers. Filed here per the user's instruction to capture decisions in the append-only log.

### 19.1 OTLP receiver = direct gRPC + stdlib net/http

**Decision.** The OTLP receivers in `internal/otlpreceiver` use `google.golang.org/grpc` directly (registering the standard OTLP collector service stubs from `go.opentelemetry.io/proto/otlp`) plus stdlib `net/http` for the HTTP surface. We do NOT pull in the OpenTelemetry Collector SDK receiver components.

**Why.** Smaller dep footprint (no Collector framework + its processors/exporters), simpler control flow, and our receive surface is narrow (three Export RPCs). The Collector SDK would help if we needed pluggable processing/export pipelines in the receiver itself; we don't.

### 19.2 OBI config writer YAML library = gopkg.in/yaml.v3

**Decision.** `internal/obiconfig` marshals via `gopkg.in/yaml.v3`. Rejected: `sigs.k8s.io/yaml` (wraps yaml.v2 with JSON-compatible tags — overkill since OBI's config schema doesn't need JSON round-tripping), encoding/json (OBI's config is YAML, not JSON).

### 19.3 OBI reload mechanism = file-watch (write-then-rename)

**Decision.** Per [`obi-integration.md`](obi-integration.md) §3.4 the agent writes OBI's config atomically (temp file + rename) and relies on OBI's built-in file watcher to pick it up. SIGHUP is the fallback if OBI's file watcher proves unreliable; not used in v0.2.

**Why.** Avoids requiring `shareProcessNamespace: true` on the pod (which has security implications). The atomic-rename approach is the standard pattern for config-file-watching daemons.

### 19.4 Reload coalescer debounce = 500 ms

**Decision.** `bridgeManager`'s reload coalescer debounces rapid AllowPID/BlockPID/EnableModule/DisableModule calls over a 500 ms quiet window before invoking the OBI config writer. Matches the value documented in `obi-integration.md` §5.

**Why.** A rollout that churns dozens of PIDs per second would otherwise produce dozens of OBI config rewrites + reload signals. 500 ms is long enough to coalesce a workload restart and short enough that operators don't perceive lag.

### 19.5 OBI image tag pinned = v0.9.0

**Decision.** The v0.2 DaemonSet manifest pins `ghcr.io/open-telemetry/obi:v0.9.0`. Future bumps follow the [ADR-0010](#adr-0010-obi-version-pinning-and-adapter) / [ADR-0018](#adr-0018-obi-as-sibling-container-not-embedded-library) single-bump-PR policy.

### 19.6 Contract test fixtures = synthetic seed first, real OBI second

**Decision.** v0.2 ships with synthetic seed fixtures (`l4-basic`, `http1-basic`) under `tests/contract/obi/testdata/translation/`, generated by `go test ./tests/contract/obi -seed -update`. The recipe for regenerating these from a real OBI sidecar (and replacing the synthetic ones) lives in `tests/contract/obi/REGENERATE.md`.

**Why.** Real-OBI recording infrastructure (Kind + OTel-collector-as-recorder + canary workload) is a multi-hour build that's not on v0.2's critical path. Synthetic seeds give CI a working contract today; real recordings displace them on the first OBI image bump.

### 19.7 pkg/capture exports TranslateMetrics / TranslateTraces

**Decision.** The OTLP→Event translators are exported from `pkg/capture` (Stability: Experimental). Embedders can call them directly for ad-hoc OTLP translation; the contract-test harness uses this to bypass the OTLP-receiver roundtrip.

**Why.** The alternative — keeping them unexported and reaching in via reflection from the contract tests — fought Go's unexported-field access rules and produced fragile test code. Exporting is cleaner, and the functions are useful enough that an embedder might want them.

### 19.8 Stale-config detection deferred

**Decision.** The "OBI rejects our config; agent backs off" detection mentioned in [`obi-integration.md`](obi-integration.md) §9 is **deferred to v0.3**. v0.2 trusts that the config it writes is valid (the YAML schema is small and exercised in unit tests); operators rely on OBI's startup logs if a config is rejected.

**Why.** Implementing reliable detection requires either reading OBI's stderr (cross-container; needs Downward API or log scraping) or polling OBI's `/healthz`. Neither is on the v0.2 critical path; v0.3's controller work is a more natural home.

---

**Implemented in.** v0.2 milestone work; each commit references the relevant sub-decision when applicable. ADR is non-superseding — these are all forward-compatible implementation choices.

---

## Open and superseded ADRs

None yet. New ADRs are appended above this section.
