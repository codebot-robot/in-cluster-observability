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
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func httpSpan(name, method, path, status string, startNs, endNs uint64) *tracepb.Span {
	return &tracepb.Span{
		Name:              name,
		StartTimeUnixNano: startNs,
		EndTimeUnixNano:   endNs,
		Attributes: []*commonpb.KeyValue{
			strKV("http.request.method", method),
			strKV("url.path", path),
			strKV("http.response.status_code", status),
		},
	}
}

func TestTranslateTraces_HTTP11(t *testing.T) {
	rs := []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				strKV("k8s.pod.name", "passes-through"),
				strKV("custom.tag", "keep-me"),
			},
		},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{
				httpSpan("GET /users/42", "GET", "/users/42", "200",
					/*start*/ 1_000_000_000 /*end*/, 1_012_000_000),
			},
		}},
	}}

	events := TranslateTraces(rs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event; got %d", len(events))
	}
	ev := events[0]
	if ev.Kind != EventSpan {
		t.Errorf("kind = %v; want span", ev.Kind)
	}
	if ev.Module != ModuleHTTP1 {
		t.Errorf("module = %v; want http1", ev.Module)
	}
	if ev.Span == nil {
		t.Fatal("Span payload nil")
	}
	if ev.Span.Method != "GET" {
		t.Errorf("Method = %q; want GET", ev.Span.Method)
	}
	if ev.Span.Path != "/users/42" {
		t.Errorf("Path = %q; want /users/42", ev.Span.Path)
	}
	if ev.Span.StatusCode != 200 {
		t.Errorf("StatusCode = %d; want 200", ev.Span.StatusCode)
	}
	if ev.Span.DurationNs != 12_000_000 {
		t.Errorf("DurationNs = %d; want 12_000_000", ev.Span.DurationNs)
	}
	if got := ev.Span.Attributes["k8s.pod.name"]; got != "passes-through" {
		t.Errorf("k8s.pod.name should pass through (ADR-0021); got %q in %v", got, ev.Span.Attributes)
	}
	if got := ev.Span.Attributes["custom.tag"]; got != "keep-me" {
		t.Errorf("custom.tag = %q; want keep-me", got)
	}
}

func TestTranslateTraces_LegacySemconv(t *testing.T) {
	// OBI image may emit either current or legacy HTTP semconv. Test
	// the legacy attribute keys still resolve.
	rs := []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				Name:              "POST /api",
				StartTimeUnixNano: 100,
				EndTimeUnixNano:   200,
				Attributes: []*commonpb.KeyValue{
					strKV("http.method", "POST"),
					strKV("http.url", "https://x/api"),
					strKV("http.status_code", "201"),
				},
			}},
		}},
	}}
	events := TranslateTraces(rs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event; got %d", len(events))
	}
	se := events[0].Span
	if se.Method != "POST" || se.StatusCode != 201 {
		t.Errorf("legacy semconv not picked up: %+v", se)
	}
	if se.Path != "https://x/api" {
		t.Errorf("Path fallback to http.url failed: %q", se.Path)
	}
}

func TestTranslateTraces_NoDuration(t *testing.T) {
	rs := []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{Name: "no-time"}}, // both timestamps zero
		}},
	}}
	events := TranslateTraces(rs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event; got %d", len(events))
	}
	if events[0].Span.DurationNs != 0 {
		t.Errorf("DurationNs = %d; want 0 when timestamps missing", events[0].Span.DurationNs)
	}
}

func TestTranslateTraces_EmptyInput(t *testing.T) {
	if got := TranslateTraces(nil); len(got) != 0 {
		t.Errorf("nil input should produce no events; got %d", len(got))
	}
}
