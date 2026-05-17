# OBI Integration

**Status:** Draft, 2026-05-17
**Owners:** TBD

This document specifies how the project consumes OpenTelemetry eBPF Instrumentation (OBI, `go.opentelemetry.io/obi`). OBI is the deepest part of the stack and the only direct dependency that explicitly promises [breaking changes between minor versions](https://opentelemetry.io/blog/2026/obi-goals/). Insulating that churn is what `core/pkg/capture` exists to do.

Background decisions: [ADR-0001](decisions.md#adr-0001-ebpf-data-plane--opentelemetry-ebpf-instrumentation-obi) (we chose OBI), [ADR-0010](decisions.md#adr-0010-obi-version-pinning-and-adapter) (we wrap it).

## 1. Goals and non-goals

**Goals:**
- One file's worth of code touches OBI APIs directly. Everything else in the project depends on `core/pkg/capture`.
- OBI version bumps are a single PR, gated by a contract-test suite.
- Embedders ([`public-api.md`](public-api.md)) never see OBI types.
- New protocol modules OBI adds can be exposed in `core/pkg/capture` with minimal change.

**Non-goals:**
- We do not aim to "abstract eBPF" generically. The adapter is specifically for OBI's shape; if we ever switch eBPF libraries, the adapter is rewritten, not extended.
- We do not aim to support multiple OBI versions concurrently. One pinned version at a time per [ADR-0010](decisions.md#adr-0010-obi-version-pinning-and-adapter).

## 2. The `core/pkg/capture` adapter

A single Go package, ~600–800 LoC, the only place that imports `go.opentelemetry.io/obi/*`.

### 2.1 Public types

```go
// Package capture wraps OBI behind a stable, project-owned interface.
//
// Stability: Stable (the interface). The contents of Config and Event
// may grow with backward-compatible additions; the existing fields are stable.
package capture

// Manager is the entry point. One per agent process.
type Manager interface {
    // Lifecycle.
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Per-PID monitoring control. Idempotent.
    AllowPID(pid uint32, spec PIDSpec) error
    BlockPID(pid uint32) error

    // Protocol module control. Idempotent.
    EnableModule(m Module, cfg ModuleConfig) error
    DisableModule(m Module) error
    EnabledModules() []Module

    // Event delivery. Records flow on this channel; closed on Stop.
    Events() <-chan Event

    // Enrichment hook (see public-api.md §4).
    AddEnricher(Enricher)

    // Self-observability handle.
    Metrics() Metrics
}

type Config struct {
    Logger      logr.Logger
    KubeletAddr string         // "https://127.0.0.1:10250" by default
    ProcPath    string         // "/proc" by default
    BpfFSPath   string         // "/sys/fs/bpf" by default
    EventBuffer int            // Events() channel size; default 4096
}

func New(Config) (Manager, error)

// Per-PID monitoring spec; controller computes and pushes via gRPC.
type PIDSpec struct {
    Protocols []Module     // which OBI tracers to attach for this PID
    Sampling  Sampling     // per-PID sampling override (optional)
    Labels    map[string]string  // additional labels attached at enrich time
}

// Module is our enum over OBI's protocol tracers.
type Module uint16
const (
    ModuleL4TCP Module = iota + 1
    ModuleHTTP1
    ModuleHTTP2
    ModuleGRPC
    ModuleTLSGoCryptoTLS
    ModuleTLSOpenSSL
    ModuleSQLPostgres
    ModuleSQLMySQL
    ModuleSQLMongoDB
    ModuleSQLRedis
    ModuleGenAI       // OpenAI/Anthropic/Gemini (per OBI)
    // ModuleKafka — roadmap
    // ModuleA2A — initially captured via ModuleHTTP*, dedicated module later
)

// Event is the project-owned shape of a single record from OBI.
// Translated from OBI's internal types by the adapter; OBI types do not leak.
type Event struct {
    Kind       EventKind   // Metric | Span | Edge
    Timestamp  time.Time
    PID        uint32
    Module     Module
    // Discriminated union — exactly one of the following is set.
    Metric *MetricEvent
    Span   *SpanEvent
    Edge   *EdgeEvent
}
```

### 2.2 What's hidden

The adapter **never** exposes:
- OBI's `Tracer`, `ProcessTracer`, or `Programs` types directly.
- OBI's internal config struct or its YAML schema.
- Raw `cilium/ebpf` `*ebpf.Program` or `*ebpf.Map` handles.
- OBI's metric/span proto types (we translate to `Event`).

### 2.3 What's mapped

The adapter is mostly a translation table. Key mappings:

| OBI surface | Our adapter |
|---|---|
| `obi.Config` | `capture.Config` (subset; defaults filled in) |
| `obiprogram.ProcessTracer.AllowPID(pid)` | `Manager.AllowPID(pid, spec)` (we also remember the spec) |
| `obi.Programs[HTTP1Tracer]` toggle | `Manager.EnableModule(ModuleHTTP1, cfg)` |
| OBI metric output (OTLP-shaped) | translated to `Event{Kind: Metric, Metric: …}` |
| OBI span output (OTLP-shaped) | translated to `Event{Kind: Span, Span: …}` |
| OBI's K8s identity attrs on records | passed through unmodified into `Event` attrs (we extend in enricher) |
| OBI ring-buffer reader | hidden; we feed our `Events()` channel from it |
| OBI panic / kernel-verifier failures | recovered, surfaced via `Manager.Metrics()` and a `Module-degraded` Event |

## 3. Version pinning policy

Per [ADR-0010](decisions.md#adr-0010-obi-version-pinning-and-adapter):

- `core/go.mod` pins **one OBI minor at a time** with an exact version (no `^` or `~`).
- OBI bumps live in their own PR. The PR is constrained to touch:
  - `core/go.mod` / `core/go.sum`
  - `core/pkg/capture/*.go` (adapter)
  - `core/tests/contract/obi/*` (contract tests, when they change)
  - `docs/design/obi-integration.md` (this file, if the surface changes)
  - `docs/design/decisions.md` (if a new ADR is needed)
- The PR description must include:
  - OBI release notes summary
  - List of any breaking changes the adapter absorbs
  - Contract-test diff (additions / removals)
- The contract test suite (§4) must pass without flakiness retries.

If a bump requires breaking changes in `pkg/capture`'s public surface, that's a MINOR-version bump for `core/` until we hit 1.0, MAJOR after. Embedders feel it; the rest of `core/` does not.

## 4. Contract test suite

Location: `core/tests/contract/obi/`. Purpose: **freeze the adapter's behavior** against recorded inputs so OBI changes can't silently regress us.

### 4.1 What contract tests are

Each test has:
- **An input fixture** — either a recorded set of kernel events (binary, captured once and committed) or a synthetic traffic generator (an in-process httptest server, a tiny gRPC server, etc.).
- **Expected `Event`s** — the exact `Event` stream the adapter must emit for that input, captured as a `golden.json` file.
- A test driver that wires the adapter against a hermetic OBI instance, runs the input, captures the actual `Event` stream, and diffs against `golden.json`.

Tests use Go's `testing` and `testdata/` conventions; goldens are regenerated by running with `-update`.

### 4.2 Test categories

| Category | What it covers | Frequency |
|---|---|---|
| **Translation** | OBI event → `Event` field mapping for every event shape we surface | Per OBI bump |
| **PID lifecycle** | AllowPID/BlockPID idempotency, double-add, BlockPID-during-attach race | Per OBI bump |
| **Module toggle** | Enable/Disable each `Module`, verify only matching events flow | Per OBI bump |
| **Panic recovery** | Force OBI panic (kernel-verifier denial fixture) and assert agent stays up, `Module-degraded` Event emitted | Per OBI bump |
| **Backpressure** | Saturate the `Events()` channel; assert metric counter ticks; no drops in OBI itself | Per OBI bump |
| **Schema stability** | Every `Event` field maps to a documented OTel semconv key | Per release |
| **Hermetic kernel** | Run inside a lightweight VM (a small QEMU image, kernel 6.x) to verify CO-RE relocation works | Nightly + per OBI bump |

### 4.3 What contract tests are not

- They are not a substitute for **end-to-end tests** (those live in [`testing-and-benchmarks.md`](testing-and-benchmarks.md)).
- They are not micro-benchmarks (separate bench harness).
- They do not test OBI itself — we trust OBI's upstream test suite for that.

## 5. Module roadmap and gap policy

OBI's protocol coverage as of v0.8 (April 2026): HTTP/1.1, HTTP/2, gRPC, SQL (PostgreSQL/pgx, MySQL, MongoDB, Redis, Couchbase), GenAI (OpenAI, Anthropic Claude, Google Gemini). 2026 OBI roadmap: MQTT, AMQP, NATS, Redis pub/sub, MongoDB extensions, cloud SDK instrumentation.

Our protocol roadmap and the policy for gaps:

| Protocol | OBI status | Our plan |
|---|---|---|
| HTTP/1.1, HTTP/2, gRPC | Shipped | Expose as `ModuleHTTP1`, `ModuleHTTP2`, `ModuleGRPC` from v1 |
| TLS — Go `crypto/tls`, OpenSSL | Shipped | Expose as `ModuleTLSGoCryptoTLS`, `ModuleTLSOpenSSL` from v1 |
| TLS — BoringSSL | Shipped (limited) | Expose with the OBI-supported subset documented |
| TLS — rustls, NSS, Java JSSE | Not in OBI | Roadmap; see [`roadmap.md`](roadmap.md) |
| SQL parsers | Shipped | Not in v1 default protocol set (off the requirements path); expose for embedders who want them |
| GenAI (OpenAI/Claude/Gemini) | Shipped | Expose as `ModuleGenAI` from v1 — directly serves the "AI agents calling AI agents" use case |
| A2A | Captured via HTTP today | `ModuleA2A` semantic layer to be added once A2A on-wire conventions stabilize |
| Kafka | Not in OBI (roadmap) | Wait for OBI; contribute upstream if we hit a need |

### Fork-vs-upstream criteria

When OBI lacks something we need:

1. **First, open an upstream issue** in `open-telemetry/opentelemetry-ebpf-instrumentation`.
2. **Next, attempt to contribute** the protocol module / fix upstream. This is the default.
3. **Maintain a downstream patch** if upstream needs more than one OBI release cycle to land. The patch lives in `core/pkg/capture/patches/` and is applied to the vendored OBI source at build time; the patch's existence is announced in release notes.
4. **Fork OBI** only if a critical hole sits unmerged after **two full OBI release cycles** (~two minor releases). A fork is an ADR-worthy decision and requires explicit user sign-off.

We never silently fork. The path is always: issue → PR → patch → fork, with explicit gates between each step.

## 6. Operational concerns

- **Vendoring.** We use Go modules, not vendor/. OBI's checked-in eBPF object files come along via `go mod` like any other Go-embedded asset.
- **eBPF generation.** OBI ships its own `bpf2go`-generated bindings. We do **not** regenerate them in our build. If we add our own `.bpf.c` (rare), it lives in `core/internal/bpf/` and follows the repo's existing `bpf2go` convention (see [`AGENTS.md`](../../AGENTS.md)).
- **CO-RE / BTF.** Both required. The adapter fails fast at startup if BTF is unavailable (per [ADR-0006](decisions.md#adr-0006-kerneldistro-target--cos-125-kernel-6x-btfco-re-required) we don't support non-BTF kernels).
- **Architecture support.** amd64 and arm64. We CI both per [`testing-and-benchmarks.md`](testing-and-benchmarks.md).
- **Permissions.** OBI requires `CAP_BPF` + `CAP_PERFMON`. Some uprobe attach paths historically needed `CAP_SYS_ADMIN`; we audit and document any actual need in [`operations.md`](operations.md).

## 7. Adapter implementation notes

Non-normative; intended to make the first cut faster.

- Single goroutine reads OBI's ringbuffer; per-event translation happens inline (no per-event allocations for small events; use `sync.Pool` for `Event` if profiling shows churn).
- `Events()` channel is closed via context cancel; back-pressure is signaled by full-channel writes incrementing `capture.dropped_events_total`.
- `AllowPID` maintains a `map[uint32]PIDSpec` under a RWMutex; module toggles are diffed against the spec on each call.
- Panic recovery wraps the OBI ringbuffer reader and the per-event handler; a recovered panic disables the responsible module (best effort — OBI doesn't always make this localizable) and emits a `Module-degraded` event.
- Module `EnableModule(ModuleTLSGoCryptoTLS, …)` performs the uprobe attach lazily on first matching process exec; the adapter handles process-watch via OBI's own facilities.

## Open questions

1. **Hermetic kernel test image.** Do we maintain our own minimal QEMU image, or piggy-back on OBI's CI? Leaning toward the latter for cost; revisit when CI ownership solidifies.
2. **Concurrent OBI versions in dev.** When prototyping an OBI bump, would a `go.mod` `replace` directive in the adapter package be useful, or should bumps always be full-repo? Likely full-repo for simplicity.
3. **A2A dedicated module.** When (not if) we add a dedicated `ModuleA2A`, it likely contributes back as an OBI protocol module rather than living in our adapter. Decision deferred to when A2A's on-wire conventions are stable enough to instrument.
