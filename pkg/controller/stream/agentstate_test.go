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

package stream_test

import (
	"testing"

	"github.com/gke-labs/in-cluster-observability/pkg/controller/stream"
)

// TestAgentStateStore_RecordAndCount covers the basic
// "agent reported active=true → count it" path.
func TestAgentStateStore_RecordAndCount(t *testing.T) {
	s := stream.NewAgentStateStore()
	s.Record("pod-1", true)
	s.Record("pod-2", true)
	s.Record("pod-3", false) // explicitly not active

	covered := map[string]bool{"pod-1": true, "pod-2": true, "pod-3": true, "pod-4": true}
	got := s.ActivelyMonitoredCount(covered)
	if got != 2 {
		t.Errorf("count = %d; want 2 (pod-1 + pod-2 active; pod-3 reported false; pod-4 never reported)", got)
	}
}

// TestAgentStateStore_RecordFalseClears: a subsequent active=false
// report drops the pod from the active set.
func TestAgentStateStore_RecordFalseClears(t *testing.T) {
	s := stream.NewAgentStateStore()
	s.Record("pod-1", true)
	s.Record("pod-1", false)
	got := s.ActivelyMonitoredCount(map[string]bool{"pod-1": true})
	if got != 0 {
		t.Errorf("count = %d; want 0 after active=false override", got)
	}
}

// TestAgentStateStore_Forget removes a pod entirely.
func TestAgentStateStore_Forget(t *testing.T) {
	s := stream.NewAgentStateStore()
	s.Record("pod-1", true)
	s.Forget("pod-1")
	got := s.ActivelyMonitoredCount(map[string]bool{"pod-1": true})
	if got != 0 {
		t.Errorf("count = %d; want 0 after Forget", got)
	}
}
