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

// Package store is the in-cluster store: a Prometheus tsdb HEAD block
// for metrics plus typed in-memory ring buffers for spans and edges.
// The concrete implementation lands in v0.3 (see ADR-0002 and
// docs/design/storage-and-query.md). v0.1 ships only the interface
// surface.
package store

import "context"

// Store is the agent-local handle exposed to the writer (per-batch
// dispatch) and the query engine (read-side fan-out).
//
// Stability: Stable
type Store interface {
	// Close flushes WAL and releases resources.
	Close(ctx context.Context) error
}
