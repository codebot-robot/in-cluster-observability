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
	"context"
	"fmt"
	"sync"
	"time"

	colllogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/gke-labs/in-cluster-observability/internal/obiconfig"
	"github.com/gke-labs/in-cluster-observability/internal/otlpreceiver"
)

// NewBridge constructs the sibling-container Manager: an OTLP receiver
// on loopback + an OBI config writer. The sibling OBI container is
// expected to push to the OTLP endpoints and watch the config file
// (per ADR-0018). NewBridge does not bind ports or write files; that
// happens in Start.
//
// Stability: Experimental
func NewBridge(cfg Config) (Manager, error) {
	cfg.applyDefaults()

	m, err := NewMetrics(cfg.MeterProvider)
	if err != nil {
		return nil, fmt.Errorf("capture: metrics init: %w", err)
	}

	b := &bridgeManager{
		cfg:     cfg,
		events:  make(chan Event, cfg.EventBuffer),
		modules: map[Module]struct{}{},
		pids:    map[uint32]PIDSpec{},
		metrics: m,
	}

	if cfg.ObiConfigPath != "" {
		w, err := obiconfig.NewWriter(cfg.ObiConfigPath)
		if err != nil {
			return nil, fmt.Errorf("capture: obi config writer: %w", err)
		}
		b.writer = w
	}

	return b, nil
}

// bridgeManager implements Manager via OTLP receivers (loopback) and
// an OBI config writer. The agent does not invoke OBI as a library;
// OBI runs as a sibling container per ADR-0018.
type bridgeManager struct {
	cfg     Config
	metrics *Metrics

	mu        sync.Mutex
	started   bool
	stopped   bool
	modules   map[Module]struct{}
	pids      map[uint32]PIDSpec
	enrichers []Enricher

	writer   *obiconfig.Writer
	receiver *otlpreceiver.Server
	events   chan Event
}

// Start binds the OTLP receivers (if addresses are configured) and
// writes the initial OBI config (if a path is configured). Returns an
// error if any step fails; partial-startup teardown is best-effort.
func (b *bridgeManager) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return ErrStopped
	}
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.mu.Unlock()

	// OTLP receiver — only bind if at least one address is configured.
	if b.cfg.OTLPGRPCAddr != "" || b.cfg.OTLPHTTPAddr != "" {
		recv, err := otlpreceiver.New(otlpreceiver.Config{
			GRPCAddr: b.cfg.OTLPGRPCAddr,
			HTTPAddr: b.cfg.OTLPHTTPAddr,
			Handler:  &bridgeHandler{b: b},
		})
		if err != nil {
			return fmt.Errorf("capture: receiver new: %w", err)
		}
		if err := recv.Start(ctx); err != nil {
			return fmt.Errorf("capture: receiver start: %w", err)
		}
		b.receiver = recv
	}

	// Initial OBI config — empty discovery list; modules are off until
	// EnableModule is called.
	if b.writer != nil {
		if _, err := b.writer.Write(obiconfig.DefaultFile(b.cfg.OBIEndpoint)); err != nil {
			return fmt.Errorf("capture: initial obi config: %w", err)
		}
	}
	return nil
}

// Stop drains the receiver and closes the Events channel. Idempotent.
func (b *bridgeManager) Stop(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	b.stopped = true
	recv := b.receiver
	b.mu.Unlock()

	if recv != nil {
		_ = recv.Stop(ctx)
	}
	close(b.events)
	return nil
}

// AllowPID is wired in #71. v0.2 stub: records the spec in memory.
// Config-writer integration lands in the AllowPID/BlockPID commit.
func (b *bridgeManager) AllowPID(pid uint32, spec PIDSpec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, existed := b.pids[pid]; !existed {
		b.metrics.ActivePIDs.Add(context.Background(), 1)
	}
	b.pids[pid] = spec
	return nil
}

// BlockPID is wired in #71. v0.2 stub: removes the spec from memory.
func (b *bridgeManager) BlockPID(pid uint32) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, existed := b.pids[pid]; existed {
		b.metrics.ActivePIDs.Add(context.Background(), -1)
	}
	delete(b.pids, pid)
	return nil
}

// EnableModule adds the module to the active set and (if a writer is
// configured) rewrites the OBI config file. Idempotent.
func (b *bridgeManager) EnableModule(m Module, _ ModuleConfig) error {
	b.mu.Lock()
	b.modules[m] = struct{}{}
	b.mu.Unlock()
	return b.reload()
}

// DisableModule removes the module and rewrites the OBI config.
func (b *bridgeManager) DisableModule(m Module) error {
	b.mu.Lock()
	delete(b.modules, m)
	b.mu.Unlock()
	return b.reload()
}

// EnabledModules returns the current module set.
func (b *bridgeManager) EnabledModules() []Module {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Module, 0, len(b.modules))
	for m := range b.modules {
		out = append(out, m)
	}
	return out
}

// Events returns the channel of translated capture events. Closed on Stop.
func (b *bridgeManager) Events() <-chan Event { return b.events }

// AddEnricher appends an enricher to the hot-path hook list.
func (b *bridgeManager) AddEnricher(e Enricher) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enrichers = append(b.enrichers, e)
}

// Metrics returns the self-observability handle.
func (b *bridgeManager) Metrics() *Metrics { return b.metrics }

// reload rewrites the OBI config from the current module/pid state.
// No-op when no writer is configured (tests / dev mode). Result is
// reported via the ObiReloadsTotal counter.
func (b *bridgeManager) reload() error {
	if b.writer == nil {
		return nil
	}
	b.mu.Lock()
	file := obiconfig.DefaultFile(b.cfg.OBIEndpoint)
	// Per #71 (AllowPID), discovery.services will be derived from pids.
	// For #70, the discovery list stays empty — module enables alone
	// don't pin specific targets.
	b.mu.Unlock()

	changed, err := b.writer.Write(file)
	if err != nil {
		b.metrics.ObiReloadsTotal.Add(context.Background(), 1)
		return fmt.Errorf("capture: reload write: %w", err)
	}
	if changed {
		b.metrics.ObiReloadsTotal.Add(context.Background(), 1)
	}
	return nil
}

// bridgeHandler implements otlpreceiver.Handler. v0.2 forwards each
// payload to the translator (#72, #73). For #70 the handler just
// counts; per-protocol translation lands in subsequent commits.
type bridgeHandler struct {
	b *bridgeManager
}

func (h *bridgeHandler) OnMetrics(ctx context.Context, _ *collmetricspb.ExportMetricsServiceRequest) error {
	h.b.metrics.EventsTotal.Add(ctx, 1)
	// Real translation lands in #72 (L4) / #73 (HTTP/1.1).
	return nil
}

func (h *bridgeHandler) OnTraces(ctx context.Context, _ *colltracepb.ExportTraceServiceRequest) error {
	h.b.metrics.EventsTotal.Add(ctx, 1)
	// Real translation lands in #73 (HTTP/1.1).
	return nil
}

func (h *bridgeHandler) OnLogs(ctx context.Context, _ *colllogspb.ExportLogsServiceRequest) error {
	h.b.metrics.EventsTotal.Add(ctx, 1)
	return nil
}

// Compile-time bookkeeping for the time import; some helpers use it.
var _ = time.Second
