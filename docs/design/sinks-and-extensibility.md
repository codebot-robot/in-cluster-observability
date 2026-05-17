# Sinks and Extensibility

**Status:** Draft, 2026-05-17
**Owners:** TBD

This document specifies how data leaves the system. It implements [ADR-0011](decisions.md#adr-0011-sink-interface-shape) and satisfies requirement §2.4 (pluggable sinks; library + controller posture).

A "sink" is anything that consumes captured records — an OTLP collector, a Prometheus server, the Kubernetes HPA via `custom.metrics.k8s.io`, an AI agent's streaming subscriber, or a third-party Go binary that imports `pkg/sink` and registers its own. All are first-class.

## 1. Three interfaces, one lifecycle

Per [ADR-0011](decisions.md#adr-0011-sink-interface-shape), the three I/O patterns get three explicit interfaces. All share a `Lifecycle`. A single sink may implement multiple interfaces.

```go
// Package sink — public registration surface.
//
// Stability: Stable
package sink

import (
    "context"
    "net/http"

    "github.com/go-logr/logr"
    "go.opentelemetry.io/otel/metric"
)

// Sink is the union; every registered sink satisfies it via at least one of
// PushSink / PullSink / StreamingSink.
type Sink interface {
    Lifecycle
}

// Lifecycle is shared. Init runs once before any traffic; Start launches any
// background goroutines; Stop drains and shuts down. All methods must be
// idempotent for retries.
type Lifecycle interface {
    Init(ctx context.Context, deps Deps) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Name() string
}

// Deps is what core gives the sink at Init time.
type Deps struct {
    Logger  logr.Logger
    Store   StoreReader   // read-only view of the in-cluster store
    Query   QueryEngine   // PromQL + CEL handles
    Metrics metric.Meter  // self-observability handle for sink-owned metrics
}

// PushSink — core hands records to the sink on each write batch.
type PushSink interface {
    Sink
    Write(ctx context.Context, batch Batch) error
}

// PullSink — the sink registers HTTP handlers; consumers scrape on demand.
type PullSink interface {
    Sink
    RegisterRoutes(mux *http.ServeMux)
}

// StreamingSink — the sink delivers a long-lived stream to one subscriber.
// Core invokes Subscribe per connection; the sink decides how it pulls from
// the store via Deps.
type StreamingSink interface {
    Sink
    Subscribe(ctx context.Context, filter string) (<-chan Event, error)
}
```

A sink struct decides which interfaces to satisfy:

```go
type myWebhookSink struct { /* ... */ }

func (s *myWebhookSink) Init(ctx context.Context, d Deps) error { /* ... */ }
func (s *myWebhookSink) Start(ctx context.Context) error        { /* ... */ }
func (s *myWebhookSink) Stop(ctx context.Context) error         { /* ... */ }
func (s *myWebhookSink) Name() string                           { return "webhook" }
func (s *myWebhookSink) Write(ctx context.Context, b Batch) error { /* POST */ }
// → PushSink only
```

## 2. The `Batch` shape

```go
type Batch struct {
    Source  Source                    // node identity for the producing agent
    Metrics []Metric                  // tsdb-shaped
    Spans   []Span                    // OTel-shaped
    Edges   []Edge                    // topology edges (see storage-and-query.md §6.3)
    Window  TimeWindow                // [start, end) covered by this batch
}

type Source struct {
    NodeName string
    PodName  string                   // the agent pod
    Cluster  string                   // operator-set
}

type TimeWindow struct {
    Start time.Time
    End   time.Time
}
```

Sinks are free to subset — a Prometheus remote-write sink ignores `Spans` and `Edges`; an OTLP sink takes all three.

Batches are immutable from the sink's perspective. Mutating the batch is a contract violation.

## 3. Registry

```go
// Stability: Stable
type Registry interface {
    Register(s Sink) error            // returns error on duplicate Name()
    Unregister(name string) error
    List() []Sink

    // Programmatic introspection for self-observability and admin endpoints.
    Status(name string) (Status, error)
}

type Status struct {
    Name             string
    Type             []SinkType        // {Push, Pull, Streaming}
    State            State             // Init | Starting | Running | Stopping | Stopped | Errored
    LastErr          error
    BatchesAccepted  uint64
    BatchesDropped   uint64
    BytesOut         uint64
    LastWrite        time.Time
}
```

`Registry` is one per process. The agent's writer calls `Registry.List()` once per batch and dispatches to all `PushSink`s. The query server iterates `PullSink`s at HTTP-mux setup. `StreamingSink`s are invoked by the streaming gRPC service on each client connection.

## 4. Backpressure and failure isolation

A misbehaving sink **must not** take down the agent. The contract:

| Failure | Sink returns / does | Core does |
|---|---|---|
| Transient error (e.g. network) | `Write` returns non-nil error | Logs, increments `ollie_sink_errors_total{name,kind="transient"}`, retries the batch up to `retryMax` (default 3) with exponential backoff. After exhaustion, drops and increments `..._dropped_total` |
| Backpressure / can't keep up | `Write` returns `sink.ErrDropped` | Logs at debug, increments `..._dropped_total`, moves on |
| Panic | uncaught panic | Recovered by core's per-sink panic handler; sink moved to `Errored` state; not retried until `Restart()` or process restart |
| Init failure | `Init` returns error | Sink is **not** registered into active rotation; logged at error; other sinks unaffected |
| Slow `Subscribe` consumer | sink's channel fills | Core drops oldest events with a per-stream counter; consumer sees gaps marked in metadata |

The agent's hot path **never blocks on a sink**. Push delivery uses a bounded per-sink buffer (default 1024 batches) with the policy above; full buffer = drop, not wait.

`PullSink.RegisterRoutes` runs at HTTP server setup; if it panics, that sink is excluded and the server continues with the others.

## 5. Built-in sinks

All built-ins live under `pkg/sink/<name>/`. Each is independently importable; the default binary registers the full set.

### 5.1 OTLP push

Package: `pkg/sink/otlp`. Implements `PushSink`.

```go
type Config struct {
    Endpoint string             // "otel-collector:4317"
    Insecure bool               // TLS off (dev)
    Headers  map[string]string  // auth tokens, etc.
    Timeout  time.Duration      // per-batch
    Compression string          // "gzip" | "" 
}

func New(c Config) sink.PushSink
```

Translates `Batch.Metrics` to OTLP `ExportMetricsServiceRequest`, `Batch.Spans` to `ExportTraceServiceRequest`, and (since edges aren't OTel-native) emits edges as spans with a well-known `ollie.kind=edge` attribute. Uses `go.opentelemetry.io/otel/exporters/otlp/*` under the hood.

### 5.2 OTLP HTTP push

Package: `pkg/sink/otlphttp`. Same as above but over OTLP/HTTP.

### 5.3 Prometheus remote-write

Package: `pkg/sink/promremote`. Implements `PushSink` (metrics only — `Spans` and `Edges` ignored).

```go
type Config struct {
    URL          string
    BearerToken  string
    BasicAuth    *BasicAuth
    TLSConfig    *TLSConfig
    QueueConfig  QueueConfig    // mirrors Prometheus remote-write tuning
}
```

Wraps `github.com/prometheus/prometheus/storage/remote`. Honors `Retry-After`. Self-observability metrics use the standard Prometheus remote-write names so existing dashboards work.

### 5.4 Prometheus scrape endpoint

Package: `pkg/sink/promscrape`. Implements `PullSink`.

`RegisterRoutes(mux)` mounts `/metrics` returning the standard Prometheus text exposition format, served directly from the tsdb HEAD via the storage's existing `/api/v1/query` plumbing repurposed. Scrape interval is set by whoever scrapes us; we don't poll.

### 5.5 Custom Metrics API (HPA)

Package: `pkg/sink/custommetrics`. Implements `PullSink`.

`RegisterRoutes(mux)` mounts the `/apis/custom.metrics.k8s.io/v1beta1/...` paths and serves the K8s custom-metrics API. Translates incoming paths to PromQL templates per [`storage-and-query.md`](storage-and-query.md#7-server-side-aggregation-for-hpa), executes against `Deps.Query`, returns `MetricValueList` JSON. Requires the `APIService` install in [`operations.md`](operations.md).

### 5.6 gRPC streaming

Package: `pkg/sink/grpcstream`. Implements `StreamingSink`.

Exposes a gRPC service with:
- `StreamSpans(filter cel) → stream Span` — long-lived, fanned out across nodes
- `StreamEdges(filter cel) → stream Edge`
- `StreamMetrics(promql, step) → stream Sample` — long-lived range query

`Subscribe(ctx, filter)` is called by the gRPC handler; the channel returned by the sink is forwarded to the client.

The CLI (`otelctl`-equivalent in `cmd/iobsctl`) speaks this gRPC service.

### 5.7 Built-in registration

The default binary registers built-ins based on `obsapi.Config`:

```go
// internal pseudo-code
if cfg.HasRole(RoleAgent) {
    registry.Register(otlp.New(cfg.OTLP))
    registry.Register(promremote.New(cfg.PromRemote))
    registry.Register(promscrape.New())  // mounted on the agent's HTTP listener
}
if cfg.HasRole(RoleQuery) {
    registry.Register(custommetrics.New())
    registry.Register(grpcstream.New())
}
```

Built-ins that an embedder doesn't want are disabled via config or by constructing the `App` without their default config block.

## 6. Library vs controller mode

There is one binary: `cmd/ollie`. The mode is set by:
- The `--role` flag (`agent | controller | query | all`), or
- `INCLUSTER_OBS_ROLE` env var, or
- `obsapi.Config.Role` when used as a library.

When used as a library, an embedder writes their own `main.go` and calls `obsapi.New(...).Run(ctx)`. All behavior is driven by which `Role` is selected and which sinks are registered.

There is **no separate "library build" vs "binary build."** The default binary is itself a library consumer that lives in this repo (the `cmd/ollie` package).

## 7. A complete third-party sink — worked example

A sink that posts every captured `Span` as a JSON payload to a webhook URL. ~40 lines:

```go
package webhook

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/gke-labs/in-cluster-observability/pkg/sink"
)

type Sink struct {
    url    string
    client *http.Client
    deps   sink.Deps
}

func New(url string) *Sink {
    return &Sink{
        url:    url,
        client: &http.Client{Timeout: 5 * time.Second},
    }
}

func (s *Sink) Name() string                           { return "webhook" }
func (s *Sink) Init(_ context.Context, d sink.Deps) error { s.deps = d; return nil }
func (s *Sink) Start(_ context.Context) error          { return nil }
func (s *Sink) Stop(_ context.Context) error           { return nil }

func (s *Sink) Write(ctx context.Context, b sink.Batch) error {
    if len(b.Spans) == 0 {
        return nil
    }
    body, err := json.Marshal(b.Spans)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := s.client.Do(req)
    if err != nil {
        return err   // core retries
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 500 {
        return fmt.Errorf("upstream: %s", resp.Status)
    }
    if resp.StatusCode == 429 {
        return sink.ErrBackoff  // core honors with longer backoff
    }
    return nil
}
```

Registration is one line in the embedder's `main.go`:

```go
app.Sinks().Register(webhook.New("https://hooks.example.com/spans"))
```

The sink inherits all the per-sink self-observability metrics for free. Adding richer behavior (e.g. an explicit retry policy, batching, auth) is local to the sink.

## 8. Configuration

Embedders configure sinks programmatically. The default binary reads a config file (YAML, see [`operations.md`](operations.md)) and instantiates the built-in sink configs from it. There is no global "registry config" file; each sink's config is its own struct, namespaced by sink name.

```yaml
# /etc/ollie/config.yaml — default binary
sinks:
  otlp:
    endpoint: otel-collector.observability:4317
    insecure: false
  promremote:
    url: https://prom.example.com/api/v1/write
    bearerTokenFile: /var/run/secrets/prom-token
  promscrape: {}       # enable with defaults
  custommetrics: {}    # enable with defaults
  grpcstream: {}       # enable with defaults
```

## 9. Self-observability

Per-sink Prometheus metrics, prefixed `ollie_sink_*` and labeled by `name`:

| Metric | Type | Notes |
|---|---|---|
| `…_batches_total{name}` | counter | batches handed to sink |
| `…_batches_dropped_total{name,reason}` | counter | reason ∈ {`buffer_full`, `retry_exhausted`, `panic`} |
| `…_errors_total{name,kind}` | counter | kind ∈ {`transient`, `permanent`} |
| `…_write_duration_seconds{name}` | histogram | per-batch sink latency |
| `…_state{name}` | gauge | enum: 0 init / 1 starting / 2 running / 3 stopping / 4 stopped / 5 errored |
| `…_subscribers{name}` | gauge | streaming sinks only |

The `Registry.Status(name)` admin endpoint surfaces the same data via HTTP/JSON for operators who don't have Prometheus wired up yet.

## 10. Versioning of the sink interface

Per [`public-api.md`](public-api.md) §3:

- The interfaces (`Lifecycle`, `PushSink`, `PullSink`, `StreamingSink`, `Sink`, `Registry`) and the types they reference (`Batch`, `Deps`, `Status`, `Source`, `TimeWindow`) are **Stable** from v1.0.
- Adding fields to `Batch`, `Deps`, `Status` is backward-compatible (Go struct extension).
- Adding new interfaces (e.g. a future `BatchPullSink`) is additive.
- Removing or renaming any of the above is a MAJOR version bump.
- The built-in sink configs (`otlp.Config`, etc.) are governed by their own package's stability tags. The OTLP and Prometheus sinks are Stable; new sinks may start Experimental.

## Open questions

1. **Per-record vs per-batch push.** Right now `PushSink.Write` takes a `Batch`. Some sinks want per-record callbacks. We could add a `RecordSink` interface later, but for v1 we believe `Batch` is right (amortizes overhead, sinks can iterate trivially). Revisit if a real use case demands it.
2. **Sink-side filtering.** Should sinks declare CEL filters they want core to apply before invoking `Write`, saving them the work? Currently sinks filter internally. Filtering at core is an optimization not a correctness fix; defer.
3. **Backpressure-aware StreamingSink.** Today `Subscribe` returns a channel; if the channel is unbuffered/small, core fills it and drops. A `Subscribe2(ctx, filter, sendFn func(Event) error) error` style might be cleaner. Decision deferred to first streaming-sink consumer at scale.
4. **Sink discovery for embedders.** Should `pkg/sink/builtin` exist as a one-import "all built-ins" convenience? Probably yes once we have 5+ built-ins; punted until then.
