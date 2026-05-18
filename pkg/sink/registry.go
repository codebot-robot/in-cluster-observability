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

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is one per process. Embedders call Register at startup; the
// agent's writer iterates registered PushSinks per batch, the query
// server iterates PullSinks at HTTP mux setup, and the streaming
// gRPC service invokes StreamingSinks per client connection.
//
// Stability: Stable
type Registry struct {
	mu    sync.RWMutex
	sinks map[string]Sink
}

// NewRegistry returns an empty Registry.
//
// Stability: Stable
func NewRegistry() *Registry {
	return &Registry{sinks: make(map[string]Sink)}
}

// Register adds a sink. Returns an error if a sink with the same Name
// is already registered.
//
// Stability: Stable
func (r *Registry) Register(s Sink) error {
	if s == nil {
		return fmt.Errorf("sink: cannot register nil")
	}
	name := s.Name()
	if name == "" {
		return fmt.Errorf("sink: Name() must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sinks[name]; exists {
		return fmt.Errorf("sink: %q already registered", name)
	}
	r.sinks[name] = s
	return nil
}

// Unregister removes a sink by name. Returns an error if no sink with
// that name is registered.
//
// Stability: Stable
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sinks[name]; !exists {
		return fmt.Errorf("sink: %q not registered", name)
	}
	delete(r.sinks, name)
	return nil
}

// List returns the registered sinks ordered by Name. The returned
// slice is a snapshot; mutating it does not affect the Registry.
//
// Stability: Stable
func (r *Registry) List() []Sink {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Sink, 0, len(r.sinks))
	for _, s := range r.sinks {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
