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

import (
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// Server implements pb.ControlPlaneServer — the controller-side end
// of the AgentSession bidirectional stream. One Server backs the
// controller's gRPC listener; per-agent state lives in *Session
// instances tracked by the shared *Dispatcher.
type Server struct {
	cppb.UnimplementedControlPlaneServer
	Dispatcher *Dispatcher

	// AgentState collects AgentStatus reports per pod UID, surfaced
	// in CR status via the reconciler's WriteStatuses pass (Phase 3
	// #93). Optional — nil means agent status feedback is dropped.
	AgentState *AgentStateStore

	// IsLeader is consulted on AgentSession entry. When false, the
	// stream is rejected with FailedPrecondition so non-leader
	// replicas don't accept agent traffic. Defaults to "always
	// leader" if nil (useful for single-replica tests).
	IsLeader func() bool
}

// AgentSession is the bidirectional streaming RPC. Lifecycle:
//
//  1. Reject if not leader.
//  2. Receive AgentHello (first message).
//  3. Construct a *Session, register with Dispatcher (which replays
//     any known specs for this node).
//  4. Spawn a goroutine that drains Session.Outbound() onto the
//     stream's Send().
//  5. Loop on stream Recv() — handle Heartbeat / Status / Digest;
//     unknown bodies ignored.
//  6. On Recv error (client gone / context done): unregister, close
//     the session, return.
func (s *Server) AgentSession(stream cppb.ControlPlane_AgentSessionServer) error {
	if s.IsLeader != nil && !s.IsLeader() {
		return status.Error(codes.FailedPrecondition, "not leader")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "first message must be AgentHello")
	}
	if hello.GetNodeName() == "" {
		return status.Error(codes.InvalidArgument, "AgentHello.node_name is required")
	}
	if err := stream.Send(&cppb.ControllerMessage{
		Body: &cppb.ControllerMessage_Hello{
			Hello: &cppb.ControllerHello{ControllerVersion: "v0.4.0-dev"},
		},
	}); err != nil {
		return err
	}
	sess := NewSession(hello.GetNodeName(), hello.GetAgentVersion())
	s.Dispatcher.RegisterSession(sess)
	defer func() {
		s.Dispatcher.UnregisterSession(sess)
		sess.close()
	}()

	// Outbound writer goroutine.
	sendErr := make(chan error, 1)
	go func() {
		for msg := range sess.Outbound() {
			if err := stream.Send(msg); err != nil {
				sendErr <- err
				return
			}
		}
		sendErr <- nil
	}()

	// Inbound reader loop.
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		// AgentStatus → AgentStateStore so the reconciler can
		// surface activelyMonitoredPodCount + per-pod Ready
		// condition (Phase 3 #93). Heartbeat / Digest are still
		// plumbed-but-not-consumed; AgentLocalDigest support lands
		// when reconnect-optimization becomes load-bearing.
		if as := msg.GetStatus(); as != nil && s.AgentState != nil {
			s.AgentState.Record(as.GetPodUid(), as.GetActive())
		}
		select {
		case err := <-sendErr:
			return err
		default:
		}
	}
}
