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

// Package sink defines the public surface third parties implement to
// receive captured records. Three I/O patterns get three explicit
// interfaces (PushSink, PullSink, StreamingSink), all sharing a
// Lifecycle. See docs/design/sinks-and-extensibility.md.
package sink

import "context"

// Sink is the union; every registered sink satisfies it via at least one
// of PushSink, PullSink, or StreamingSink.
//
// Stability: Stable
type Sink interface {
	Lifecycle
}

// Lifecycle is shared. Init runs once before any traffic; Start launches
// any background goroutines; Stop drains and shuts down. All methods
// must be idempotent for retries.
//
// Stability: Stable
type Lifecycle interface {
	Init(ctx context.Context, deps Deps) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Name() string
}

// Deps is what core gives the sink at Init time. v0.1 ships an empty
// Deps; logger, store, query, and metrics handles arrive with the
// matching subsystems in later milestones.
//
// Stability: Stable
type Deps struct{}
