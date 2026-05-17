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
	"fmt"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// MeterName is the canonical instrumentation scope for the capture
// subsystem's self-observability metrics. Per ADR-0017.2 we use the
// OpenTelemetry metrics SDK; the Prometheus scrape sink in v0.3
// re-exports these via otel/exporters/prometheus.
//
// Stability: Stable
const MeterName = "github.com/gke-labs/in-cluster-observability/pkg/capture"

// NewMetrics constructs a Metrics handle from the supplied
// MeterProvider. Pass a noop provider for tests or for the no-op
// Manager. Returns an error only if instrument creation fails
// (e.g. duplicate name with conflicting type).
//
// Stability: Experimental
func NewMetrics(mp metric.MeterProvider) (*Metrics, error) {
	if mp == nil {
		mp = noop.NewMeterProvider()
	}
	meter := mp.Meter(MeterName)

	m := &Metrics{Meter: meter}

	var err error
	if m.EventsTotal, err = meter.Int64Counter(
		"ollie_capture_events_total",
		metric.WithDescription("Total capture events emitted to the Events() channel, by module and kind."),
	); err != nil {
		return nil, fmt.Errorf("capture: events_total: %w", err)
	}
	if m.EventsDroppedTotal, err = meter.Int64Counter(
		"ollie_capture_events_dropped_total",
		metric.WithDescription("Capture events not delivered, by reason."),
	); err != nil {
		return nil, fmt.Errorf("capture: events_dropped_total: %w", err)
	}
	if m.ActivePIDs, err = meter.Int64UpDownCounter(
		"ollie_capture_active_pids",
		metric.WithDescription("Number of PIDs currently in the AllowPID set."),
	); err != nil {
		return nil, fmt.Errorf("capture: active_pids: %w", err)
	}
	if m.ObiReloadsTotal, err = meter.Int64Counter(
		"ollie_capture_obi_reloads_total",
		metric.WithDescription("OBI sibling-container config-reload signals issued, by result."),
	); err != nil {
		return nil, fmt.Errorf("capture: obi_reloads_total: %w", err)
	}
	if m.ObiRestartsTotal, err = meter.Int64Counter(
		"ollie_capture_obi_restarts_total",
		metric.WithDescription("OBI sibling-container restart count, sourced from container-status watcher."),
	); err != nil {
		return nil, fmt.Errorf("capture: obi_restarts_total: %w", err)
	}
	if m.PanicsTotal, err = meter.Int64Counter(
		"ollie_capture_panics_total",
		metric.WithDescription("Recovered panics in the capture pipeline, by component."),
	); err != nil {
		return nil, fmt.Errorf("capture: panics_total: %w", err)
	}

	return m, nil
}

// DefaultMeterProvider returns an in-process SDK MeterProvider with no
// exporters wired up. Suitable for unit tests and for the default
// binary in v0.2 (the Prometheus scrape sink in v0.3 adds the exporter).
//
// Stability: Experimental
func DefaultMeterProvider() *sdkmetric.MeterProvider {
	return sdkmetric.NewMeterProvider()
}
