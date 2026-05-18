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

// Package stream hosts the controller-side gRPC stream server that
// agents connect into and the dispatcher that fans MonitoringSpec
// changes out to the per-node agent sessions.
//
// The reconciler (pkg/controller/reconciler) computes
// per-pod MonitoringSpecs and calls Dispatcher.Apply with the
// node-keyed result; Dispatcher diffs against last-known spec per
// pod and pushes UPSERT / REMOVE deltas onto the relevant agent
// session's outbound channel.
package stream

import (
	"sync"

	"google.golang.org/protobuf/proto"

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// Dispatcher is the controller-side fan-out: reconciler calls
// Apply with the latest per-pod MonitoringSpec set; Dispatcher diffs
// against last-known specs per pod, decides UPSERT / REMOVE, and
// pushes deltas onto the per-node agent session outbound channel.
//
// Dispatcher is goroutine-safe; the reconciler and the gRPC stream
// server share one Dispatcher.
type Dispatcher struct {
	mu sync.Mutex

	// lastSpec holds the spec last sent to (or queued for) each pod.
	// Keyed by pod UID. Entries are removed on REMOVE deltas.
	lastSpec map[string]*cppb.MonitoringSpec

	// generation is the monotonic delta counter, also keyed by pod
	// UID. The agent ignores deltas with generation <= last seen.
	generation map[string]int64

	// sessions are the currently-connected agent sessions, keyed by
	// node name. Reconciler-driven deltas are dispatched to the
	// session whose node_name matches the pod's NodeName.
	sessions map[string]*Session
}

// NewDispatcher constructs an empty Dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		lastSpec:   map[string]*cppb.MonitoringSpec{},
		generation: map[string]int64{},
		sessions:   map[string]*Session{},
	}
}

// RegisterSession wires a connected agent session into the
// dispatcher's node-keyed map. Called by the stream server on
// AgentHello receipt. If a session for the same node is already
// registered (stale leftover), the old session is closed.
func (d *Dispatcher) RegisterSession(s *Session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.sessions[s.NodeName]; ok && prev != s {
		prev.close()
	}
	d.sessions[s.NodeName] = s
	// On a fresh registration, replay the known specs for this
	// node so the agent gets a complete picture without waiting
	// for the next reconcile event.
	for uid, spec := range d.lastSpec {
		if spec.GetNodeName() != s.NodeName {
			continue
		}
		s.enqueue(&cppb.ControllerMessage{
			Body: &cppb.ControllerMessage_SpecDelta{
				SpecDelta: &cppb.MonitoringSpecDelta{
					Op:         cppb.MonitoringSpecDelta_UPSERT,
					Spec:       proto.Clone(spec).(*cppb.MonitoringSpec),
					Generation: d.generation[uid],
				},
			},
		})
	}
}

// UnregisterSession removes a session on disconnect (only if it
// still matches what's registered for the node — a newer session
// may have already replaced it).
func (d *Dispatcher) UnregisterSession(s *Session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, ok := d.sessions[s.NodeName]; ok && cur == s {
		delete(d.sessions, s.NodeName)
	}
}

// Apply replaces the controller's view of which pods should be
// monitored with the supplied per-pod-UID map. Pods absent from the
// map but present in lastSpec receive a REMOVE delta. Pods whose
// spec changed receive an UPSERT delta. Pods whose spec is byte-
// identical to last produce no delta (idempotent).
//
// `current` is keyed by pod UID. Each spec must have a non-empty
// NodeName (the dispatch key).
func (d *Dispatcher) Apply(current map[string]*cppb.MonitoringSpec) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// UPSERT: anything new or changed.
	for uid, spec := range current {
		prev := d.lastSpec[uid]
		if prev != nil && proto.Equal(prev, spec) {
			continue
		}
		d.generation[uid]++
		d.lastSpec[uid] = proto.Clone(spec).(*cppb.MonitoringSpec)
		if sess, ok := d.sessions[spec.GetNodeName()]; ok {
			sess.enqueue(&cppb.ControllerMessage{
				Body: &cppb.ControllerMessage_SpecDelta{
					SpecDelta: &cppb.MonitoringSpecDelta{
						Op:         cppb.MonitoringSpecDelta_UPSERT,
						Spec:       proto.Clone(spec).(*cppb.MonitoringSpec),
						Generation: d.generation[uid],
					},
				},
			})
		}
	}
	// REMOVE: anything in lastSpec but not in current.
	for uid, prev := range d.lastSpec {
		if _, kept := current[uid]; kept {
			continue
		}
		d.generation[uid]++
		nodeName := prev.GetNodeName()
		delete(d.lastSpec, uid)
		if sess, ok := d.sessions[nodeName]; ok {
			sess.enqueue(&cppb.ControllerMessage{
				Body: &cppb.ControllerMessage_SpecDelta{
					SpecDelta: &cppb.MonitoringSpecDelta{
						Op:         cppb.MonitoringSpecDelta_REMOVE,
						Spec:       &cppb.MonitoringSpec{PodUid: uid, NodeName: nodeName},
						Generation: d.generation[uid],
					},
				},
			})
		}
	}
}

// SessionCount returns the number of currently-connected agent
// sessions. Exposed for the controller's self-obs metrics.
func (d *Dispatcher) SessionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sessions)
}
