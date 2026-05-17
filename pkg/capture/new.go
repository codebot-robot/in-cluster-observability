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
	"sync"
)

// New returns a v0.1 no-op Manager. Real OBI wiring lands in v0.2.
//
// Stability: Experimental
func New(cfg Config) (Manager, error) {
	return &noopManager{
		cfg:     cfg,
		events:  make(chan Event),
		modules: map[Module]struct{}{},
		pids:    map[uint32]PIDSpec{},
	}, nil
}

// noopManager is the v0.1 placeholder: lifecycle is real, but no
// kernel work happens. Events() is closed on Stop without ever
// emitting an event.
type noopManager struct {
	cfg Config

	mu       sync.Mutex
	started  bool
	stopped  bool
	modules  map[Module]struct{}
	pids     map[uint32]PIDSpec
	enrichers []Enricher

	events chan Event
}

func (m *noopManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return errStopped
	}
	m.started = true
	return nil
}

func (m *noopManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil
	}
	m.stopped = true
	close(m.events)
	return nil
}

func (m *noopManager) AllowPID(pid uint32, spec PIDSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pids[pid] = spec
	return nil
}

func (m *noopManager) BlockPID(pid uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pids, pid)
	return nil
}

func (m *noopManager) EnableModule(mod Module, _ ModuleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modules[mod] = struct{}{}
	return nil
}

func (m *noopManager) DisableModule(mod Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.modules, mod)
	return nil
}

func (m *noopManager) EnabledModules() []Module {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Module, 0, len(m.modules))
	for mod := range m.modules {
		out = append(out, mod)
	}
	return out
}

func (m *noopManager) Events() <-chan Event { return m.events }

func (m *noopManager) AddEnricher(e Enricher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enrichers = append(m.enrichers, e)
}

func (m *noopManager) Metrics() Metrics { return nil }
