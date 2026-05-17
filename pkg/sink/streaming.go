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

package sink

import "context"

// StreamingSink — the sink delivers a long-lived stream of events to
// one subscriber per call to Subscribe. Examples: AI agents, otelctl.
//
// Stability: Stable
type StreamingSink interface {
	Sink
	Subscribe(ctx context.Context, filter string) (<-chan Event, error)
}

// Event is the per-record envelope core feeds into a StreamingSink's
// channel. Concrete payload types fill in alongside their owning
// subsystems.
//
// Stability: Experimental
type Event struct {
	Kind string // "metric" | "span" | "edge"
}
