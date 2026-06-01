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

package debugendpoint_test

import (
	"testing"

	"github.com/gke-labs/in-cluster-observability/internal/debugendpoint"
	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

func TestTraceStore(t *testing.T) {
	ts := debugendpoint.NewTraceStore(3)

	span1 := &capture.SpanEvent{
		Name:        "GET /users",
		Method:      "GET",
		Path:        "/users",
		StatusCode:  200,
		DurationNs:  1000,
		TraceID:     "trace1",
		SpanID:      "span1",
		StartTimeNs: 100,
		EndTimeNs:   1100,
	}
	span2 := &capture.SpanEvent{
		Name:        "GET /users/1",
		Method:      "GET",
		Path:        "/users/1",
		StatusCode:  200,
		DurationNs:  2000,
		TraceID:     "trace1",
		SpanID:      "span2",
		StartTimeNs: 200,
		EndTimeNs:   2200,
	}
	span3 := &capture.SpanEvent{
		Name:        "POST /items",
		Method:      "POST",
		Path:        "/items",
		StatusCode:  201,
		DurationNs:  1500,
		TraceID:     "trace2",
		SpanID:      "span3",
		StartTimeNs: 150,
		EndTimeNs:   1650,
	}
	span4 := &capture.SpanEvent{
		Name:        "GET /items/1",
		Method:      "GET",
		Path:        "/items/1",
		StatusCode:  200,
		DurationNs:  500,
		TraceID:     "trace3",
		SpanID:      "span4",
		StartTimeNs: 300,
		EndTimeNs:   350,
	}

	ts.Add(span1)
	ts.Add(span2)
	ts.Add(span3)

	all := ts.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(all))
	}
	if all[0].SpanID != "span1" || all[1].SpanID != "span2" || all[2].SpanID != "span3" {
		t.Errorf("incorrect order in All(): %v", all)
	}

	// Test ring buffer limit behavior
	ts.Add(span4)
	all = ts.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 spans after adding 4th, got %d", len(all))
	}
	if all[0].SpanID != "span2" || all[1].SpanID != "span3" || all[2].SpanID != "span4" {
		t.Errorf("incorrect order in All() after wrap: %v", all)
	}

	// Test GetTraceSpans
	t1spans := ts.GetTraceSpans("trace1")
	if len(t1spans) != 1 {
		t.Fatalf("expected 1 span for trace1, got %d (since span1 was evicted)", len(t1spans))
	}
	if t1spans[0].SpanID != "span2" {
		t.Errorf("expected span2, got %s", t1spans[0].SpanID)
	}

	// Test GetTraceSummaries
	sums := ts.GetTraceSummaries()
	if len(sums) != 3 {
		t.Fatalf("expected 3 trace summaries, got %d", len(sums))
	}
	// Summaries should be sorted by StartTimeNs descending: trace3 (300) -> trace1 (200) -> trace2 (150)
	if sums[0].TraceID != "trace3" || sums[1].TraceID != "trace1" || sums[2].TraceID != "trace2" {
		t.Errorf("incorrect summary order: %v", sums)
	}
}
