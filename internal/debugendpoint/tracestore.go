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

package debugendpoint

import (
	"sort"
	"sync"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

// TraceStore is an in-memory ring buffer for capture.SpanEvent.
// Thread-safe.
type TraceStore struct {
	mu    sync.RWMutex
	spans []*capture.SpanEvent
	limit int
	head  int
}

// NewTraceStore constructs a TraceStore with the specified capacity limit.
func NewTraceStore(limit int) *TraceStore {
	if limit <= 0 {
		limit = 1000
	}
	return &TraceStore{
		spans: make([]*capture.SpanEvent, 0, limit),
		limit: limit,
	}
}

// Add appends a span event to the store.
func (ts *TraceStore) Add(s *capture.SpanEvent) {
	if s == nil {
		return
	}
	// Ignore empty or all-zero trace IDs
	if s.TraceID == "" || s.TraceID == "00000000000000000000000000000000" {
		return
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.spans) < ts.limit {
		ts.spans = append(ts.spans, s)
	} else {
		ts.spans[ts.head] = s
		ts.head = (ts.head + 1) % ts.limit
	}
}

// All returns a slice of all spans currently in the store, sorted from oldest to newest.
func (ts *TraceStore) All() []*capture.SpanEvent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	n := len(ts.spans)
	out := make([]*capture.SpanEvent, n)
	if n < ts.limit {
		copy(out, ts.spans)
	} else {
		for i := 0; i < n; i++ {
			out[i] = ts.spans[(ts.head+i)%ts.limit]
		}
	}
	return out
}

// TraceSummary represents a high-level summary of a trace.
type TraceSummary struct {
	TraceID      string            `json:"trace_id"`
	RootSpanName string            `json:"root_span_name"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	StatusCode   int               `json:"status_code"`
	StartTimeNs  uint64            `json:"start_time_ns"`
	EndTimeNs    uint64            `json:"end_time_ns"`
	DurationNs   uint64            `json:"duration_ns"`
	SpanCount    int               `json:"span_count"`
	ServiceName  string            `json:"service_name,omitempty"`
}

// GetTraceSummaries groups all spans in the store by TraceID and returns a summary for each trace,
// sorted by start_time_ns descending (newest first).
func (ts *TraceStore) GetTraceSummaries() []TraceSummary {
	spans := ts.All()

	// Group spans by TraceID
	groups := make(map[string][]*capture.SpanEvent)
	for _, s := range spans {
		groups[s.TraceID] = append(groups[s.TraceID], s)
	}

	summaries := make([]TraceSummary, 0, len(groups))
	for traceID, gSpans := range groups {
		if len(gSpans) == 0 {
			continue
		}

		// Sort spans in this trace by StartTimeNs to find the earliest/root span
		sort.Slice(gSpans, func(i, j int) bool {
			return gSpans[i].StartTimeNs < gSpans[j].StartTimeNs
		})

		earliest := gSpans[0]
		minStart := earliest.StartTimeNs
		maxEnd := earliest.EndTimeNs

		for _, s := range gSpans {
			if s.StartTimeNs < minStart {
				minStart = s.StartTimeNs
			}
			if s.EndTimeNs > maxEnd {
				maxEnd = s.EndTimeNs
			}
		}

		// Find service name from attributes of the earliest/root span
		serviceName := earliest.Attributes["service.name"]
		if serviceName == "" {
			serviceName = earliest.Attributes["k8s.pod.name"]
		}

		summaries = append(summaries, TraceSummary{
			TraceID:      traceID,
			RootSpanName: earliest.Name,
			Method:       earliest.Method,
			Path:         earliest.Path,
			StatusCode:   earliest.StatusCode,
			StartTimeNs:  minStart,
			EndTimeNs:    maxEnd,
			DurationNs:   maxEnd - minStart,
			SpanCount:    len(gSpans),
			ServiceName:  serviceName,
		})
	}

	// Sort summaries by start_time_ns descending
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartTimeNs > summaries[j].StartTimeNs
	})

	return summaries
}

// GetTraceSpans returns all spans for a specific trace, sorted by StartTimeNs ascending.
func (ts *TraceStore) GetTraceSpans(traceID string) []*capture.SpanEvent {
	spans := ts.All()
	var out []*capture.SpanEvent
	for _, s := range spans {
		if s.TraceID == traceID {
			out = append(out, s)
		}
	}

	// Sort spans by StartTimeNs ascending
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTimeNs < out[j].StartTimeNs
	})

	return out
}
