// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sink

import (
	"context"
	"errors"
	"time"
)

// PushSink — core hands records to the sink on each write batch.
//
// Stability: Stable
type PushSink interface {
	Sink
	Write(ctx context.Context, batch Batch) error
}

// Batch is the immutable unit core hands to a PushSink. Sinks may
// subset (e.g. a metrics-only sink ignores Spans and Edges).
//
// Stability: Stable
type Batch struct {
	Source  Source
	Metrics []Metric
	Spans   []Span
	Edges   []Edge
	Window  TimeWindow
}

// Source identifies the agent that produced the batch.
//
// Stability: Stable
type Source struct {
	NodeName string
	PodName  string
	Cluster  string
}

// TimeWindow is the [Start, End) interval covered by a batch.
//
// Stability: Stable
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

// Metric is the on-wire shape for tsdb-bound metrics. Field set lands
// with the store implementation in v0.3.
//
// Stability: Experimental
type Metric struct{}

// Span is the on-wire shape for OTel-shaped spans from L7 captures.
// Field set lands with the capture implementation in v0.2.
//
// Stability: Experimental
type Span struct{}

// Edge is the on-wire shape for topology edges. Field set lands with
// the topology subsystem in v0.5.
//
// Stability: Experimental
type Edge struct{}

// ErrDropped is the sentinel a PushSink returns when it can't keep up.
// Core records a counter and continues; the batch is not retried.
//
// Stability: Stable
var ErrDropped = errors.New("sink: batch dropped due to backpressure")

// ErrBackoff is the sentinel a PushSink returns to ask core to apply
// longer-than-default backoff before the next retry (e.g. after a 429).
//
// Stability: Stable
var ErrBackoff = errors.New("sink: upstream asked for backoff")
