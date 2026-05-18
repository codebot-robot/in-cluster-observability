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

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
	"github.com/gke-labs/in-cluster-observability/pkg/controller/stream"
)

// collect drains a session's outbound channel without blocking
// indefinitely. Returns up to `max` messages.
func collect(s *stream.Session, max int) []*cppb.ControllerMessage {
	out := make([]*cppb.ControllerMessage, 0, max)
	for i := 0; i < max; i++ {
		select {
		case msg, ok := <-s.Outbound():
			if !ok {
				return out
			}
			out = append(out, msg)
		default:
			return out
		}
	}
	return out
}

func spec(podUID, nodeName string, ports ...uint32) *cppb.MonitoringSpec {
	return &cppb.MonitoringSpec{
		PodUid:    podUID,
		NodeName:  nodeName,
		Protocols: 0b11,
		HttpPorts: ports,
	}
}

// TestDispatcher_ApplyEmitsUpserts confirms a fresh Apply call
// produces one UPSERT per pod, dispatched to the session whose node
// matches.
func TestDispatcher_ApplyEmitsUpserts(t *testing.T) {
	d := stream.NewDispatcher()
	s := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s)

	d.Apply(map[string]*cppb.MonitoringSpec{
		"pod-1": spec("pod-1", "node-a", 80),
		"pod-2": spec("pod-2", "node-a", 8080),
	})

	got := collect(s, 10)
	if len(got) != 2 {
		t.Fatalf("got %d msgs; want 2", len(got))
	}
	for _, msg := range got {
		delta := msg.GetSpecDelta()
		if delta == nil {
			t.Errorf("msg has no SpecDelta: %v", msg)
			continue
		}
		if delta.GetOp() != cppb.MonitoringSpecDelta_UPSERT {
			t.Errorf("Op = %v; want UPSERT", delta.GetOp())
		}
	}
}

// TestDispatcher_ApplyIdempotent: re-applying byte-identical specs
// produces no new deltas (each pod's generation does not bump).
func TestDispatcher_ApplyIdempotent(t *testing.T) {
	d := stream.NewDispatcher()
	s := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s)

	input := map[string]*cppb.MonitoringSpec{"pod-1": spec("pod-1", "node-a", 80)}
	d.Apply(input)
	collect(s, 10) // drain the initial upsert

	d.Apply(input)
	got := collect(s, 10)
	if len(got) != 0 {
		t.Errorf("got %d msgs on idempotent re-apply; want 0", len(got))
	}
}

// TestDispatcher_ApplyEmitsRemove: a pod present in lastSpec but
// absent from the new Apply set produces a REMOVE delta.
func TestDispatcher_ApplyEmitsRemove(t *testing.T) {
	d := stream.NewDispatcher()
	s := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s)

	d.Apply(map[string]*cppb.MonitoringSpec{
		"pod-1": spec("pod-1", "node-a", 80),
		"pod-2": spec("pod-2", "node-a", 8080),
	})
	collect(s, 10)

	d.Apply(map[string]*cppb.MonitoringSpec{
		"pod-1": spec("pod-1", "node-a", 80),
	})

	got := collect(s, 10)
	if len(got) != 1 {
		t.Fatalf("got %d msgs; want 1 REMOVE", len(got))
	}
	delta := got[0].GetSpecDelta()
	if delta == nil || delta.GetOp() != cppb.MonitoringSpecDelta_REMOVE {
		t.Errorf("expected REMOVE; got %v", delta)
	}
	if delta.GetSpec().GetPodUid() != "pod-2" {
		t.Errorf("REMOVE for wrong pod: %v", delta.GetSpec().GetPodUid())
	}
}

// TestDispatcher_RegisterReplaysExistingSpecs: when a session
// (re)registers, the dispatcher replays the known specs for that
// node so the agent gets a complete picture without waiting for the
// next reconcile.
func TestDispatcher_RegisterReplaysExistingSpecs(t *testing.T) {
	d := stream.NewDispatcher()
	// Spec arrives before any session connects (e.g. controller was
	// up first; agent comes up later).
	d.Apply(map[string]*cppb.MonitoringSpec{"pod-1": spec("pod-1", "node-a", 80)})

	s := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s)

	got := collect(s, 10)
	if len(got) != 1 {
		t.Fatalf("got %d msgs; want 1 replay UPSERT", len(got))
	}
	if got[0].GetSpecDelta().GetOp() != cppb.MonitoringSpecDelta_UPSERT {
		t.Errorf("expected UPSERT replay; got %v", got[0])
	}
}

// TestDispatcher_NodeRoutingFilter: a spec for node-b doesn't land
// on a session for node-a.
func TestDispatcher_NodeRoutingFilter(t *testing.T) {
	d := stream.NewDispatcher()
	sa := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(sa)

	d.Apply(map[string]*cppb.MonitoringSpec{
		"pod-1": spec("pod-1", "node-b", 80), // intentionally wrong node
	})

	got := collect(sa, 10)
	if len(got) != 0 {
		t.Errorf("got %d msgs on node-a for a node-b pod; want 0", len(got))
	}
}

// TestDispatcher_DisplacedSessionClosed: registering a new session
// for the same node closes the prior one.
func TestDispatcher_DisplacedSessionClosed(t *testing.T) {
	d := stream.NewDispatcher()
	s1 := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s1)

	s2 := stream.NewSession("node-a", "v0.4.0")
	d.RegisterSession(s2)

	// s1's outbound channel should now be closed; reading it should
	// return immediately with !ok.
	select {
	case _, ok := <-s1.Outbound():
		if ok {
			t.Error("s1.Outbound() returned a message; expected closed channel")
		}
	default:
		t.Error("s1.Outbound() did not return immediately; expected closed channel")
	}
}
