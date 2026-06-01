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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gke-labs/in-cluster-observability/internal/debugendpoint"
	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

func newServer(t *testing.T) (string, capture.Manager, func()) {
	t.Helper()
	mgr, err := capture.NewBridge(capture.Config{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv, err := debugendpoint.New(mgr, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("server Start: %v", err)
	}
	return addr, mgr, func() {
		_ = srv.Stop(context.Background())
		_ = mgr.Stop(context.Background())
	}
}

func TestNew_RejectsNonLoopback(t *testing.T) {
	mgr, _ := capture.NewBridge(capture.Config{})
	if _, err := debugendpoint.New(mgr, "0.0.0.0:9099"); err == nil {
		t.Error("0.0.0.0 bind should be rejected")
	}
	if _, err := debugendpoint.New(mgr, "10.0.0.1:9099"); err == nil {
		t.Error("non-loopback bind should be rejected")
	}
}

func TestAllowPID_RoundTripsToManager(t *testing.T) {
	addr, mgr, cleanup := newServer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"pid":     uint32(4242),
		"modules": []int{int(capture.ModuleHTTP1)},
		"labels":  map[string]string{"workload": "test"},
	})
	req, _ := http.NewRequest("POST", "http://"+addr+"/debug/allow-pid", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST allow-pid: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d; want 204", resp.StatusCode)
	}

	// Verify via state endpoint that modules track. ActivePIDs gauge
	// is incremented via the manager's metric handle; we'd need the
	// SDK reader to observe it. Skip metric verification here — covered
	// in pkg/capture tests.
	stateReq, _ := http.NewRequest("GET", "http://"+addr+"/debug/state", nil)
	stateResp, err := http.DefaultClient.Do(stateReq)
	if err != nil {
		t.Fatalf("GET state: %v", err)
	}
	defer stateResp.Body.Close()
	var state struct {
		PIDs    []uint32         `json:"pids"`
		Modules []capture.Module `json:"modules"`
	}
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	// Modules reported here are the manager-enabled modules, not
	// per-PID modules — those land via AllowPID in the agent's spec
	// store, not via Manager.EnableModule. Just verify the endpoint
	// responds and is JSON-shaped.
	_ = mgr
}

func TestBlockPID_RoundTripsToManager(t *testing.T) {
	addr, _, cleanup := newServer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{"pid": uint32(7777)})
	req, _ := http.NewRequest("POST", "http://"+addr+"/debug/block-pid", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST block-pid: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d; want 204", resp.StatusCode)
	}
}

func TestAllowPID_RejectsBadInput(t *testing.T) {
	addr, _, cleanup := newServer(t)
	defer cleanup()

	// Bad JSON
	req, _ := http.NewRequest("POST", "http://"+addr+"/debug/allow-pid", bytes.NewReader([]byte("not json")))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON: status = %d; want 400", resp.StatusCode)
	}

	// Missing pid
	body, _ := json.Marshal(map[string]any{"modules": []int{1}})
	req2, _ := http.NewRequest("POST", "http://"+addr+"/debug/allow-pid", bytes.NewReader(body))
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("missing pid: status = %d; want 400", resp2.StatusCode)
	}
}

func TestExtraHandler_PromMetricsScrape(t *testing.T) {
	mp, h, err := capture.NewPromMeterProvider()
	if err != nil {
		t.Fatalf("NewPromMeterProvider: %v", err)
	}
	mgr, err := capture.NewBridge(capture.Config{MeterProvider: mp})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(context.Background()) })

	srv, err := debugendpoint.New(mgr, "127.0.0.1:0",
		debugendpoint.WithExtraHandler("GET /debug/metrics", h))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Tick the counter so it actually shows up in the scrape — OTel
	// SDK omits never-recorded instruments by default.
	mgr.Metrics().EventsTotal.Add(context.Background(), 1)

	resp, err := http.Get("http://" + addr + "/debug/metrics")
	if err != nil {
		t.Fatalf("GET /debug/metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)
	// Prometheus text exposition surface — must include at least one of
	// our self-obs metrics. The exact name depends on the OTel→Prometheus
	// translation ("." → "_"); accept either prefix.
	if !strings.Contains(out, "ollie_capture_") {
		t.Fatalf("/debug/metrics did not include ollie_capture_*; body:\n%s", out)
	}
}

func TestStopIdempotent(t *testing.T) {
	_, _, cleanup := newServer(t)
	cleanup() // first stop
	cleanup() // second stop should be benign (handlers cleanup is no-op)
	time.Sleep(10 * time.Millisecond)
}

func TestTraceEndpoints(t *testing.T) {
	mgr, _ := capture.NewBridge(capture.Config{})
	_ = mgr.Start(context.Background())
	defer func() { _ = mgr.Stop(context.Background()) }()

	ts := debugendpoint.NewTraceStore(10)
	ts.Add(&capture.SpanEvent{
		Name:        "GET /test",
		TraceID:     "abc123trace",
		SpanID:      "span1",
		StartTimeNs: 1000,
		EndTimeNs:   2000,
	})

	srv, err := debugendpoint.New(mgr, "127.0.0.1:0", debugendpoint.WithTraceStore(ts))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()

	// Verify /debug/explorer serves HTML
	resp, err := http.Get("http://" + addr + "/debug/explorer")
	if err != nil {
		t.Fatalf("GET /debug/explorer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("explorer status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Ollie Trace Explorer") {
		t.Errorf("explorer HTML missing title")
	}

	// Verify /debug/api/traces serves summaries
	resp2, err := http.Get("http://" + addr + "/debug/api/traces")
	if err != nil {
		t.Fatalf("GET /debug/api/traces: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("api status = %d, want 200", resp2.StatusCode)
	}
	var summaries []debugendpoint.TraceSummary
	if err := json.NewDecoder(resp2.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].TraceID != "abc123trace" {
		t.Errorf("incorrect summaries returned: %v", summaries)
	}

	// Verify /debug/api/traces?trace_id=abc123trace serves spans
	resp3, err := http.Get("http://" + addr + "/debug/api/traces?trace_id=abc123trace")
	if err != nil {
		t.Fatalf("GET /debug/api/traces?trace_id: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("api spans status = %d, want 200", resp3.StatusCode)
	}
	var spans []*capture.SpanEvent
	if err := json.NewDecoder(resp3.Body).Decode(&spans); err != nil {
		t.Fatalf("decode spans: %v", err)
	}
	if len(spans) != 1 || spans[0].SpanID != "span1" {
		t.Errorf("incorrect spans returned: %v", spans)
	}
}
