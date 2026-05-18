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

package stream

import "sync"

// AgentStateStore tracks AgentStatus messages reported by agents.
// Keyed by pod UID. The reconciler reads via ActivelyMonitoredCount
// when writing CR status (Phase 3 #93).
//
// This is a small, focused store separate from Dispatcher's
// generation / spec tracking — Dispatcher writes; AgentStateStore
// reads from the agent side. Both are goroutine-safe.
type AgentStateStore struct {
	mu     sync.RWMutex
	active map[string]bool // pod_uid → AgentStatus.active
}

// NewAgentStateStore constructs an empty store.
func NewAgentStateStore() *AgentStateStore {
	return &AgentStateStore{active: map[string]bool{}}
}

// Record persists one AgentStatus report. Called by the gRPC server
// on each AgentStatus received on the wire.
func (s *AgentStateStore) Record(podUID string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active {
		s.active[podUID] = true
	} else {
		delete(s.active, podUID)
	}
}

// Forget drops a pod's entry (called when the agent reports REMOVE
// success or when the controller's coverage no longer includes the
// pod and the dispatcher emits a REMOVE delta).
func (s *AgentStateStore) Forget(podUID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, podUID)
}

// ActivelyMonitoredCount counts how many of the supplied pod UIDs
// have an active AgentStatus on file. Implements reconciler.AgentReporter.
func (s *AgentStateStore) ActivelyMonitoredCount(podUIDs map[string]bool) int32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var n int32
	for uid := range podUIDs {
		if s.active[uid] {
			n++
		}
	}
	return n
}
