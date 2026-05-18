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

package main

import (
	"context"
	"sync"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
	"github.com/gke-labs/in-cluster-observability/pkg/controller/agentclient"
	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// captureSink implements agentclient.Sink. It receives MonitoringSpec
// UPSERT / REMOVE deltas from the controller's gRPC stream and turns
// them into AllowPID / BlockPID calls on the capture.Manager — which
// in turn rewrites OBI's discovery.instrument config and triggers an
// OBI reload via the existing v0.2 coalescer.
//
// v0.4 simplification: the controller doesn't yet supply PID hints,
// so we synthesize a deterministic "pseudo-PID" per pod UID (top of
// the uint32 space). This lets the existing AllowPID-keyed flow
// stand up end-to-end while real PID resolution lands as agent-side
// work in a later milestone. The OBI side ends up with one
// discovery.instrument entry per allow-listed pod-UID; OBI's
// open_ports + exe_path matching does the actual process selection.
//
// The protocols bitset on MonitoringSpec is unused in v0.4 — the
// agent's existing --obi-instrument-ports seed already covers the
// L4 + HTTP/1.1 protocols v0.4 ships, and per-protocol toggles
// arrive with v0.6's richer Module surface.
type captureSink struct {
	mgr capture.Manager

	mu       sync.Mutex
	podToPID map[string]uint32 // pod_uid → synthetic PID
	nextPID  uint32
}

func newCaptureSink(mgr capture.Manager) *captureSink {
	return &captureSink{
		mgr:      mgr,
		podToPID: map[string]uint32{},
		// Start at 1<<31 — far above any real PID. v0.4 OBI uses
		// the PID as a selector key; collisions with real PIDs
		// would be a problem if the controller-driven path
		// instrumented the same process twice. The high half-space
		// gives ~2B unique pseudo-PIDs.
		nextPID: 1 << 31,
	}
}

func (s *captureSink) OnUpsert(_ context.Context, spec *cppb.MonitoringSpec) error {
	s.mu.Lock()
	pid, ok := s.podToPID[spec.GetPodUid()]
	if !ok {
		pid = s.nextPID
		s.nextPID++
		s.podToPID[spec.GetPodUid()] = pid
	}
	s.mu.Unlock()
	return s.mgr.AllowPID(pid, capture.PIDSpec{
		Labels: map[string]string{
			"k8s.pod.uid":        spec.GetPodUid(),
			"k8s.pod.name":       spec.GetPodName(),
			"k8s.namespace.name": spec.GetNamespace(),
			"ollie.source":       spec.GetSourceRef(),
		},
	})
}

func (s *captureSink) OnRemove(_ context.Context, podUID string) error {
	s.mu.Lock()
	pid, ok := s.podToPID[podUID]
	if ok {
		delete(s.podToPID, podUID)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return s.mgr.BlockPID(pid)
}

// runControllerClient is called from cmd/ollie's main when
// --controller-addr is set. Blocks until ctx is canceled.
func runControllerClient(ctx context.Context, addr, nodeName string, mgr capture.Manager, logf func(string, ...any)) {
	client, err := agentclient.New(agentclient.Config{
		ControllerAddr:   addr,
		NodeName:         nodeName,
		AgentVersion:     version,
		SupportedModules: []string{"l4_tcp", "http1"},
		Sink:             newCaptureSink(mgr),
		Logf:             logf,
	})
	if err != nil {
		logf("controller client init failed: %v", err)
		return
	}
	client.Run(ctx)
}
