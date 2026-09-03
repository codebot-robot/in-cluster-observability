# OpenTelemetry Parquet Archive Layout Contract

This document specifies the schema, object path structure, physical layout, and compression settings of the **Parquet** archive produced by `opentelemetry-compactor`. 

This layout serves as a formal contract so that downstream query systems (e.g., DuckDB, ClickHouse, any standard Parquet reader) can query the cold-tier storage efficiently without external catalogs or central indexing.

## Object Layout (Hive-Partitioning)

Compacted Parquet files are written to the object storage bucket (e.g. S3/GCS) under a Hive-partitioned directory structure based on the **record timestamp** (in UTC) rather than the raw shard upload time.

```
<prefix>/parquet/signal=<logs|traces|metrics>/date=YYYY-MM-DD/hour=HH/<part>.parquet
```

- **`signal`**: One of `logs`, `traces`, or `metrics`.
- **`date`**: `YYYY-MM-DD` representation of the record timestamp in UTC.
- **`hour`**: Two-digit `HH` representation of the record hour in UTC (00-23).
- **`<part>`**: Formatted as `part-<timestamp_nano>-<part_index>.parquet` to ensure uniqueness across concurrent runs and avoid collisions.

## Physical Storage Settings (Pruning & Stats)

All query performance and data pruning come from metadata written at compaction time. No external indexing is used.

1. **Sort Order Within Files**: Sorted by `(ServiceName, Timestamp)` across all three signals. This makes row-group min/max statistics highly selective for both time-range queries and service-specific queries.
2. **Compression**: ZStandard (`zstd`) compression.
3. **Row Groups**: Target of `100,000` to `200,000` rows per row group.
4. **Bloom Filters**:
   - `TraceId` on **traces** and **logs** files.
   - `K8sPod` on **all three signals** (logs, traces, metrics).
   This allows point lookups and pod-scoped queries to prune row groups and pages without downloading entire files.
5. **File Size Split Limit**: Target of `128MB` to `256MB` per part file. If partition rows exceed this limit, they are automatically split into sequential parts (`part-*-0.parquet`, `part-*-1.parquet`, etc.).

---

## Schemas

The schemas align closely with the de-facto standard OpenTelemetry ClickHouse exporter layouts, with promoted Kubernetes infrastructure columns to avoid file explosion while retaining query efficiency.

### 1. Logs Schema

| Column Name | Physical Type | Logical Type / Precision / Format | Notes |
|-------------|---------------|----------------------------------|-------|
| `Timestamp` | `int64` | `TIMESTAMP(nanosecond)` | UTC timestamp of log |
| `TraceId` | `string` | UTF-8 String | Hex-encoded string representation |
| `SpanId` | `string` | UTF-8 String | Hex-encoded string representation |
| `TraceFlags` | `uint32` | 32-bit Unsigned Integer | |
| `SeverityText` | `string` | UTF-8 String | Friendly severity |
| `SeverityNumber` | `int32` | 32-bit Signed Integer | Numeric severity enum |
| `ServiceName` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sNamespace` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sPod` | `string` | UTF-8 String | Promoted from resource attrs |
| `Body` | `string` | UTF-8 String | Serialized log body |
| `ResourceSchemaUrl` | `string` | UTF-8 String | Resource schema URL |
| `ResourceAttributes`| `map<string, string>` | Key-Value Map | All resource attributes |
| `ScopeName` | `string` | UTF-8 String | Instrumentation scope name |
| `ScopeVersion` | `string` | UTF-8 String | Instrumentation scope version |
| `ScopeAttributes`| `map<string, string>`| Key-Value Map | All scope attributes |
| `LogAttributes` | `map<string, string>`| Key-Value Map | All log record attributes |

### 2. Traces Schema

| Column Name | Physical Type | Logical Type / Precision / Format | Notes |
|-------------|---------------|----------------------------------|-------|
| `Timestamp` | `int64` | `TIMESTAMP(nanosecond)` | Span start timestamp |
| `Duration` | `int64` | `INT64` | Duration in nanoseconds |
| `TraceId` | `string` | UTF-8 String | Hex-encoded string representation |
| `SpanId` | `string` | UTF-8 String | Hex-encoded string representation |
| `ParentSpanId` | `string` | UTF-8 String | Hex-encoded parent span ID |
| `TraceState` | `string` | UTF-8 String | Trace state string |
| `SpanName` | `string` | UTF-8 String | Name of the span |
| `SpanKind` | `string` | UTF-8 String | friendly span kind |
| `ServiceName` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sNamespace` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sPod` | `string` | UTF-8 String | Promoted from resource attrs |
| `ResourceAttributes`| `map<string, string>` | Key-Value Map | All resource attributes |
| `ScopeName` | `string` | UTF-8 String | Instrumentation scope name |
| `ScopeVersion` | `string` | UTF-8 String | Instrumentation scope version |
| `SpanAttributes`| `map<string, string>`| Key-Value Map | All span attributes |
| `StatusCode` | `string` | UTF-8 String | friendly status code string |
| `StatusMessage` | `string` | UTF-8 String | friendly status message |
| `Events` | `list<struct>` | `SpanEvent` List | Trace events list |
| `Links` | `list<struct>` | `SpanLink` List | Trace links list |

#### Nested Types:

**`SpanEvent` structural layout**:
- `Timestamp`: `int64` (`TIMESTAMP(nanosecond)`)
- `Name`: `string`
- `Attributes`: `map<string, string>`

**`SpanLink` structural layout**:
- `TraceId`: `string` (Hex-encoded)
- `SpanId`: `string` (Hex-encoded)
- `TraceState`: `string`
- `Attributes`: `map<string, string>`

### 3. Metrics Schema (Unified "points" Table)

Covers `gauge` and `sum` metrics. Histogram, exponential-histogram, and summary metrics are ignored by this version of the compactor (counted/logged as skipped) and are preserved in their raw format in the bucket.

| Column Name | Physical Type | Logical Type / Precision / Format | Notes |
|-------------|---------------|----------------------------------|-------|
| `Timestamp` | `int64` | `TIMESTAMP(nanosecond)` | Point data timestamp |
| `StartTimestamp` | `int64` | `TIMESTAMP(nanosecond)` | Point start timestamp |
| `MetricName` | `string` | UTF-8 String | Metric identifier |
| `MetricDescription` | `string`| UTF-8 String | Description of the metric |
| `MetricUnit` | `string` | UTF-8 String | Unit of measurement |
| `MetricType` | `string` | UTF-8 String | `"gauge"` or `"sum"` |
| `AggregationTemporality` | `string`| UTF-8 String | Sum aggregation temporality |
| `IsMonotonic` | `bool` | `BOOLEAN` | True if sum is monotonic |
| `Value` | `double` | `DOUBLE` | widened float64 value |
| `Flags` | `uint32` | 32-bit Unsigned Integer | |
| `ServiceName` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sNamespace` | `string` | UTF-8 String | Promoted from resource attrs |
| `K8sPod` | `string` | UTF-8 String | Promoted from resource attrs |
| `ResourceAttributes`| `map<string, string>` | Key-Value Map | All resource attributes |
| `ScopeName` | `string` | UTF-8 String | Instrumentation scope name |
| `ScopeVersion` | `string` | UTF-8 String | Instrumentation scope version |
| `Attributes` | `map<string, string>`| Key-Value Map | Metric point attributes |

---

## Idempotency and Bookkeeping

To guarantee transactional safety and make execution runs completely idempotent:

- A dedicated marker file `<prefix>/compacted/<pod>/<shard-name>.compacted` is written to commit the compaction of a shard.
- Shards are only marked as compacted after **all** their partitioned records have been successfully written and uploaded.
- If a late-arriving shard for a previously compacted hour is encountered, it is successfully compacted into an additional Part file under the same partition, without losing or corrupting existing data.
