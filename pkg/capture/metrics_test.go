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
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

func TestNewMetrics_AllInstrumentsPresent(t *testing.T) {
	m, err := capture.NewMetrics(noop.NewMeterProvider())
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	if m.Meter == nil ||
		m.EventsTotal == nil ||
		m.EventsDroppedTotal == nil ||
		m.ActivePIDs == nil ||
		m.ObiReloadsTotal == nil ||
		m.ObiRestartsTotal == nil ||
		m.PanicsTotal == nil {
		t.Fatal("NewMetrics returned a handle with nil instruments")
	}
}

func TestNewMetrics_NilProviderDefaultsToNoop(t *testing.T) {
	m, err := capture.NewMetrics(nil)
	if err != nil {
		t.Fatalf("NewMetrics(nil): %v", err)
	}
	if m.EventsTotal == nil {
		t.Fatal("NewMetrics(nil) should default to noop and still build instruments")
	}
}

// TestNewMetrics_CountersRecord uses the SDK with a manual reader to
// verify the counters actually accumulate and emit datapoints with the
// expected names.
func TestNewMetrics_CountersRecord(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(t.Context()) }()

	m, err := capture.NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	ctx := t.Context()
	m.EventsTotal.Add(ctx, 5)
	m.EventsDroppedTotal.Add(ctx, 2)
	m.ActivePIDs.Add(ctx, 3)
	m.ObiReloadsTotal.Add(ctx, 1)
	m.PanicsTotal.Add(ctx, 1)

	var out metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &out); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(out.ScopeMetrics) == 0 {
		t.Fatal("no scope metrics produced; expected the capture scope")
	}

	want := map[string]bool{
		"ollie_capture_events_total":         false,
		"ollie_capture_events_dropped_total": false,
		"ollie_capture_active_pids":          false,
		"ollie_capture_obi_reloads_total":    false,
		"ollie_capture_panics_total":         false,
	}
	for _, sm := range out.ScopeMetrics {
		for _, mt := range sm.Metrics {
			if _, ok := want[mt.Name]; ok {
				want[mt.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected metric %q not produced", name)
		}
	}
}
