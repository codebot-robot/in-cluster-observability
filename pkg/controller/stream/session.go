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
	"sync"

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// Session represents one connected agent. Lifecycle is owned by the
// gRPC stream server (Server.AgentSession): the server constructs
// the Session on AgentHello, hands it to Dispatcher.RegisterSession,
// loops on the outbound channel to push messages to the wire, and
// calls Dispatcher.UnregisterSession + close on stream teardown.
type Session struct {
	NodeName     string
	AgentVersion string

	// out is the per-session outbound queue. Bounded; if it fills
	// up, deltas are dropped and DropCount increments — the agent
	// recovers via AgentLocalDigest on reconnect.
	out chan *cppb.ControllerMessage

	mu        sync.Mutex
	closed    bool
	dropCount uint64
}

// NewSession constructs a Session with a buffered outbound channel.
// Buffer size is generous — per-pod deltas are tiny, and the worst
// case (a thousand-pod node churning all at once) is still well
// under 64 KiB of message data.
func NewSession(nodeName, agentVersion string) *Session {
	return &Session{
		NodeName:     nodeName,
		AgentVersion: agentVersion,
		out:          make(chan *cppb.ControllerMessage, 1024),
	}
}

// Outbound exposes the per-session channel the gRPC server's send
// loop reads from. Closed by close().
func (s *Session) Outbound() <-chan *cppb.ControllerMessage { return s.out }

// DropCount returns the number of messages dropped because the
// outbound channel was full. Exposed for self-obs.
func (s *Session) DropCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropCount
}

// enqueue is called by the Dispatcher; non-blocking send so a slow
// or stuck agent never blocks the reconciler.
func (s *Session) enqueue(msg *cppb.ControllerMessage) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	select {
	case s.out <- msg:
	default:
		s.mu.Lock()
		s.dropCount++
		s.mu.Unlock()
	}
}

// close is called by the dispatcher on session displacement or by
// the server on stream teardown. Idempotent.
func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.out)
}
