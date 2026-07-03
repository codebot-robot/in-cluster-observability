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

package capture_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

// bridgeWithRestart is a thin assertion helper. We use the public
// ReportOBIRestart API (on the concrete bridge type, exposed via
// type assertion).
type restartReporter interface {
	ReportOBIRestart(ctx context.Context, restartCount int64)
}

func TestReportOBIRestart_BelowThreshold_NoDegradeEvent(t *testing.T) {
	mgr, err := capture.NewBridge(capture.Config{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(t.Context())
	_ = mgr.EnableModule(capture.ModuleL4TCP, capture.ModuleConfig{})

	r, ok := mgr.(restartReporter)
	if !ok {
		t.Fatal("bridge manager should implement ReportOBIRestart")
	}
	r.ReportOBIRestart(t.Context(), 1)
	r.ReportOBIRestart(t.Context(), 2)

	select {
	case ev := <-mgr.Events():
		if ev.Kind == capture.EventModuleDegraded {
			t.Errorf("ModuleDegraded fired below threshold: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		// OK — no event expected.
	}
}

func TestReportOBIRestart_AtThreshold_EmitsDegradedPerModule(t *testing.T) {
	mgr, err := capture.NewBridge(capture.Config{EventBuffer: 16})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(t.Context())

	_ = mgr.EnableModule(capture.ModuleL4TCP, capture.ModuleConfig{})
	_ = mgr.EnableModule(capture.ModuleHTTP1, capture.ModuleConfig{})

	r := mgr.(restartReporter)
	r.ReportOBIRestart(t.Context(), 3) // ≥ threshold

	got := map[capture.Module]bool{}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for len(got) < 2 {
		select {
		case ev := <-mgr.Events():
			if ev.Kind == capture.EventModuleDegraded {
				got[ev.Module] = true
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for ModuleDegraded events; got %v", got)
		}
	}
	if !got[capture.ModuleL4TCP] || !got[capture.ModuleHTTP1] {
		t.Errorf("expected ModuleDegraded for both L4 and HTTP1; got %v", got)
	}
}

func TestReportOBIRestart_NoModulesEnabled_NoDegrade(t *testing.T) {
	mgr, _ := capture.NewBridge(capture.Config{})
	if err := mgr.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(t.Context())
	r := mgr.(restartReporter)
	r.ReportOBIRestart(t.Context(), 10)

	select {
	case ev := <-mgr.Events():
		if ev.Kind == capture.EventModuleDegraded {
			t.Errorf("ModuleDegraded fired with no enabled modules: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		// OK.
	}
}

// errSentinel is unused but kept to anchor errors import if added later.
var errSentinel = errors.New("sentinel")
var _ = errSentinel
