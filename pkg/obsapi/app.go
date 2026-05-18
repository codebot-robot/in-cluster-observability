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

// Package obsapi is the one-stop embedder facade described in
// docs/design/public-api.md §2. It composes capture, store, query,
// sink, topology, and controller into a single App handle so
// embedders can avoid wiring seven packages by hand. Power users
// still reach into the sub-packages directly.
//
// In v0.1 the App methods return no-op handles; the underlying
// subsystems wire in real implementations as their milestones land.
package obsapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
	"github.com/gke-labs/in-cluster-observability/pkg/query"
	"github.com/gke-labs/in-cluster-observability/pkg/sink"
	"github.com/gke-labs/in-cluster-observability/pkg/store"
	"github.com/gke-labs/in-cluster-observability/pkg/topology"
)

// App is the embedder's handle on a configured ollie
// process. It exposes each subsystem; embedders pick what they need.
//
// Stability: Stable
type App struct {
	cfg Config

	capture  capture.Manager
	store    store.Store
	query    query.Engine
	sinks    *sink.Registry
	topology topology.Resolver
}

// New constructs an App from the supplied Config. Returns an error if
// the Role is zero (no subsystems requested) or if subsystem
// construction fails.
//
// Stability: Stable
func New(cfg Config) (*App, error) {
	cfg = cfg.Defaults()
	if cfg.Role == 0 {
		return nil, errors.New("obsapi: Config.Role must select at least one subsystem")
	}

	mgr, err := capture.New(capture.Config{})
	if err != nil {
		return nil, fmt.Errorf("obsapi: capture: %w", err)
	}

	return &App{
		cfg:     cfg,
		capture: mgr,
		sinks:   sink.NewRegistry(),
		// store, query, topology arrive in their respective milestones.
	}, nil
}

// Capture returns the agent's eBPF capture manager. In RoleAgent
// processes this is the live Manager; in other roles it is the no-op
// Manager from v0.1.
//
// Stability: Stable
func (a *App) Capture() capture.Manager { return a.capture }

// Store returns the in-cluster store handle. Nil in v0.1.
//
// Stability: Stable
func (a *App) Store() store.Store { return a.store }

// Query returns the query engine handle. Nil in v0.1.
//
// Stability: Stable
func (a *App) Query() query.Engine { return a.query }

// Sinks returns the sink Registry shared by every subsystem in this
// process.
//
// Stability: Stable
func (a *App) Sinks() *sink.Registry { return a.sinks }

// Topology returns the K8s identity resolver. Nil in v0.1.
//
// Stability: Stable
func (a *App) Topology() topology.Resolver { return a.topology }

// Config returns a copy of the effective configuration (defaults
// already applied).
//
// Stability: Stable
func (a *App) Config() Config { return a.cfg }

// Run starts every subsystem the configured Role activates and blocks
// until ctx is done. In v0.1 it starts the no-op capture manager,
// waits on ctx, then stops it.
//
// Stability: Stable
func (a *App) Run(ctx context.Context) error {
	if err := a.capture.Start(ctx); err != nil {
		return fmt.Errorf("obsapi: capture.Start: %w", err)
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.capture.Stop(stopCtx); err != nil {
		return fmt.Errorf("obsapi: capture.Stop: %w", err)
	}
	return ctx.Err()
}
