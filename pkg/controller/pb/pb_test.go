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

package pb_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// TestProtoRoundTrip exercises Marshal/Unmarshal on the central
// MonitoringSpecDelta to catch breakage in the generated stubs.
// Phase 1 doesn't have a controller or stream yet — this is just a
// smoke check that the generated code compiles and the wire format
// is symmetric.
func TestProtoRoundTrip(t *testing.T) {
	in := &cppb.MonitoringSpecDelta{
		Op:         cppb.MonitoringSpecDelta_UPSERT,
		Generation: 7,
		Spec: &cppb.MonitoringSpec{
			PodUid:    "abc-123",
			PodName:   "nginx-567b68cc5f-6mggl",
			Namespace: "demo",
			NodeName:  "ollie-v03-control-plane",
			Protocols: 0b11,
			HttpPorts: []uint32{80, 443},
			SourceRef: "TrafficMonitor demo/nginx",
		},
	}
	bytes, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out cppb.MonitoringSpecDelta
	if err := proto.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, want := out.Op, cppb.MonitoringSpecDelta_UPSERT; got != want {
		t.Errorf("Op = %v; want %v", got, want)
	}
	if got, want := out.Generation, int64(7); got != want {
		t.Errorf("Generation = %d; want %d", got, want)
	}
	if got, want := out.Spec.GetPodUid(), "abc-123"; got != want {
		t.Errorf("Spec.PodUid = %q; want %q", got, want)
	}
	if got, want := out.Spec.GetHttpPorts(), []uint32{80, 443}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Spec.HttpPorts = %v; want %v", got, want)
	}
}

// TestServiceDescriptor confirms the ControlPlane service is
// registered in the generated stubs. Without this, a typo'd proto
// service name would slip through silently until the stream wired
// up in Phase 2.
func TestServiceDescriptor(t *testing.T) {
	desc := cppb.ControlPlane_ServiceDesc
	if got, want := desc.ServiceName, "ollie.controlplane.v1.ControlPlane"; got != want {
		t.Errorf("ServiceName = %q; want %q", got, want)
	}
	if len(desc.Streams) != 1 {
		t.Fatalf("expected 1 streaming method; got %d", len(desc.Streams))
	}
	if got, want := desc.Streams[0].StreamName, "AgentSession"; got != want {
		t.Errorf("Streams[0] = %q; want AgentSession", got)
	}
	if !desc.Streams[0].ClientStreams || !desc.Streams[0].ServerStreams {
		t.Errorf("AgentSession must be bidirectional; got client=%v server=%v",
			desc.Streams[0].ClientStreams, desc.Streams[0].ServerStreams)
	}
}
