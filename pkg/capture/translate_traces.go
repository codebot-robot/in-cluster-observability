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

package capture

import (
	"strconv"
	"strings"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// TranslateTraces walks an OTLP ResourceSpans tree and emits a
// capture.Event{Kind:Span} per Span. Per ADR-0017.5 (HTTP/1.1
// focused), spans are decoded with the minimal field set: method,
// path (raw), status, duration_ns.
//
// OBI's exact attribute keys vary by semconv version. We accept both
// the current `http.request.method` / `url.path` /
// `http.response.status_code` shape and the older `http.method` /
// `http.url` / `http.status_code` shape.
func TranslateTraces(rs []*tracepb.ResourceSpans) []Event {
	var out []Event
	now := time.Now()
	for _, r := range rs {
		resAttrs := stripK8sAttrs(kvToMap(r.GetResource().GetAttributes()))
		for _, ss := range r.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				attrs := mergeMaps(resAttrs, stripK8sAttrs(kvToMap(sp.GetAttributes())))
				se := &SpanEvent{
					Name:       sp.GetName(),
					Method:     pickFirst(attrs, "http.request.method", "http.method"),
					Path:       pickFirst(attrs, "url.path", "http.url", "http.target"),
					StatusCode: parseStatus(pickFirst(attrs, "http.response.status_code", "http.status_code")),
					DurationNs: spanDurationNs(sp),
					Attributes: attrs,
				}
				out = append(out, Event{
					Kind:      EventSpan,
					Timestamp: now,
					Module:    ModuleHTTP1,
					Span:      se,
				})
			}
		}
	}
	return out
}

// pickFirst returns the first non-empty value across keys; "" if none.
func pickFirst(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// parseStatus turns an HTTP status string into an int; 0 if empty/bad.
func parseStatus(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// spanDurationNs returns (end - start) in nanoseconds; 0 if either
// timestamp is unset or end <= start.
func spanDurationNs(sp *tracepb.Span) uint64 {
	start := sp.GetStartTimeUnixNano()
	end := sp.GetEndTimeUnixNano()
	if start == 0 || end == 0 || end <= start {
		return 0
	}
	return end - start
}
