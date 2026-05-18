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

// Package agentclient is the agent-side gRPC client for the
// controller↔agent AgentSession stream. cmd/ollie starts a Client
// when --controller-addr is set; the Client dials the controller,
// loops on the bidirectional stream, and dispatches received
// MonitoringSpec deltas onto the supplied Sink (which the agent
// wires into capture.Manager.AllowPID / BlockPID).
package agentclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
)

// streamSender wraps a gRPC stream's Send with a mutex so the
// heartbeat goroutine and the handle-loop AgentStatus sends don't
// race. gRPC streams allow concurrent Send + Recv but require
// Sends to be serialized.
type streamSender struct {
	stream cppb.ControlPlane_AgentSessionClient
	mu     sync.Mutex
}

func (s *streamSender) Send(msg *cppb.AgentMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

// Sink is the per-delta consumer the agent supplies — typically a
// shim around capture.Manager.AllowPID / BlockPID. Keeping the
// interface narrow lets us unit-test the client without a real
// capture pipeline.
type Sink interface {
	OnUpsert(ctx context.Context, spec *cppb.MonitoringSpec) error
	OnRemove(ctx context.Context, podUID string) error
}

// Config governs the agent-side client.
type Config struct {
	// ControllerAddr is the gRPC target (e.g. "ollie-controller.ollie-system.svc:9101").
	// Empty value disables the client entirely (caller responsibility to check).
	ControllerAddr string

	// NodeName is the K8s node the agent is running on. Sent in
	// AgentHello so the controller can route deltas for pods on
	// this node to this session.
	NodeName string

	// AgentVersion is reported in AgentHello for operator visibility.
	AgentVersion string

	// SupportedModules lists capture.Module names this agent's OBI
	// build can attach. Controller may filter MonitoringSpec deltas
	// to modules this list includes.
	SupportedModules []string

	// Sink consumes received deltas.
	Sink Sink

	// ReconnectBackoff is the initial reconnect delay. Doubles per
	// failure up to MaxReconnectBackoff. Default 1s.
	ReconnectBackoff time.Duration

	// MaxReconnectBackoff caps the reconnect delay. Default 30s.
	MaxReconnectBackoff time.Duration

	// HeartbeatInterval is how often the agent sends AgentHeartbeat.
	// Default 5s.
	HeartbeatInterval time.Duration

	// Logf is the structured-ish log sink. Defaults to a no-op.
	Logf func(format string, args ...any)
}

func (c *Config) applyDefaults() {
	if c.ReconnectBackoff <= 0 {
		c.ReconnectBackoff = time.Second
	}
	if c.MaxReconnectBackoff <= 0 {
		c.MaxReconnectBackoff = 30 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
}

// Client is the long-running agent → controller gRPC stream
// consumer. Construct via New, start with Run (blocks until ctx
// done). Run reconnects with exponential backoff on any stream
// error and stays alive across controller leader failover.
type Client struct {
	cfg Config
}

// New constructs a Client. Validates that ControllerAddr is set; the
// caller is expected to check at flag-parse time too.
func New(cfg Config) (*Client, error) {
	if cfg.ControllerAddr == "" {
		return nil, errors.New("agentclient: ControllerAddr is required")
	}
	if cfg.NodeName == "" {
		return nil, errors.New("agentclient: NodeName is required")
	}
	if cfg.Sink == nil {
		return nil, errors.New("agentclient: Sink is required")
	}
	cfg.applyDefaults()
	return &Client{cfg: cfg}, nil
}

// Run dials the controller and runs the AgentSession stream loop
// until ctx is canceled. Reconnects with exponential backoff on
// any stream error or controller leader change (FailedPrecondition).
func (c *Client) Run(ctx context.Context) {
	backoff := c.cfg.ReconnectBackoff
	for ctx.Err() == nil {
		if err := c.session(ctx); err != nil && ctx.Err() == nil {
			c.cfg.Logf("agentclient: session ended: %v; retry in %v", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		// Exponential backoff with cap. Reset to base after a clean
		// (no-error) session — handled by session() returning nil.
		if backoff < c.cfg.MaxReconnectBackoff {
			backoff *= 2
			if backoff > c.cfg.MaxReconnectBackoff {
				backoff = c.cfg.MaxReconnectBackoff
			}
		}
	}
}

// session runs one connection / stream. Returns when the stream
// terminates (clean or error) or when ctx is canceled. Idiomatic
// returns: nil on clean teardown (EOF after ctx cancel), error
// otherwise.
func (c *Client) session(ctx context.Context) error {
	// v0.4: insecure dial. cert-manager + TLS lands with the
	// validating webhook in v0.5; until then the controller's
	// gRPC listener is in-cluster ClusterIP-only (per
	// docs/design/control-plane.md §10).
	conn, err := grpc.NewClient(c.cfg.ControllerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial %q: %w", c.cfg.ControllerAddr, err)
	}
	defer conn.Close()

	cp := cppb.NewControlPlaneClient(conn)
	raw, err := cp.AgentSession(ctx)
	if err != nil {
		return fmt.Errorf("open AgentSession: %w", err)
	}
	sender := &streamSender{stream: raw}
	if err := sender.Send(&cppb.AgentMessage{
		Body: &cppb.AgentMessage_Hello{
			Hello: &cppb.AgentHello{
				NodeName:         c.cfg.NodeName,
				AgentVersion:     c.cfg.AgentVersion,
				SupportedModules: c.cfg.SupportedModules,
			},
		},
	}); err != nil {
		return fmt.Errorf("send AgentHello: %w", err)
	}

	heartbeatTick := time.NewTicker(c.cfg.HeartbeatInterval)
	defer heartbeatTick.Stop()
	heartbeatErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				heartbeatErr <- nil
				return
			case <-heartbeatTick.C:
				if err := sender.Send(&cppb.AgentMessage{
					Body: &cppb.AgentMessage_Heartbeat{
						Heartbeat: &cppb.AgentHeartbeat{},
					},
				}); err != nil {
					heartbeatErr <- err
					return
				}
			}
		}
	}()

	for {
		msg, err := raw.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			if s, ok := status.FromError(err); ok && s.Code() == codes.FailedPrecondition {
				// Not leader — let the backoff loop reconnect; the
				// agent's next dial may land on the new leader.
				return fmt.Errorf("controller not leader: %s", s.Message())
			}
			return fmt.Errorf("recv: %w", err)
		}
		c.handle(ctx, sender, msg)
		select {
		case err := <-heartbeatErr:
			if err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		default:
		}
	}
}

// handle dispatches one ControllerMessage to the sink and, on
// MonitoringSpecDelta application, sends an AgentStatus back up
// the stream so the controller can populate
// activelyMonitoredPodCount in CR status (Phase 3 #93). Per-delta
// sink errors are logged but never tear down the session — repeated
// errors will trigger controller-side resync logic eventually.
func (c *Client) handle(ctx context.Context, s *streamSender, msg *cppb.ControllerMessage) {
	if h := msg.GetHello(); h != nil {
		c.cfg.Logf("agentclient: connected to controller %s", h.GetControllerVersion())
		return
	}
	delta := msg.GetSpecDelta()
	if delta == nil {
		return
	}
	switch delta.GetOp() {
	case cppb.MonitoringSpecDelta_UPSERT:
		err := c.cfg.Sink.OnUpsert(ctx, delta.GetSpec())
		c.sendStatus(s, delta.GetSpec().GetPodUid(), err == nil, err)
		if err != nil {
			c.cfg.Logf("agentclient: upsert pod=%s: %v", delta.GetSpec().GetPodUid(), err)
		}
	case cppb.MonitoringSpecDelta_REMOVE:
		err := c.cfg.Sink.OnRemove(ctx, delta.GetSpec().GetPodUid())
		// REMOVE always reports active=false (the agent is no
		// longer monitoring the pod), regardless of whether the
		// local BlockPID call errored.
		c.sendStatus(s, delta.GetSpec().GetPodUid(), false, err)
		if err != nil {
			c.cfg.Logf("agentclient: remove pod=%s: %v", delta.GetSpec().GetPodUid(), err)
		}
	}
}

// sendStatus reports one AgentStatus back to the controller. Send
// errors are logged but not fatal — the session's outbound side
// will surface a hard error via heartbeatErr.
func (c *Client) sendStatus(s *streamSender, podUID string, active bool, sinkErr error) {
	msg := &cppb.AgentStatus{PodUid: podUID, Active: active}
	if sinkErr != nil {
		msg.Error = sinkErr.Error()
	}
	if err := s.Send(&cppb.AgentMessage{
		Body: &cppb.AgentMessage_Status{Status: msg},
	}); err != nil {
		c.cfg.Logf("agentclient: send AgentStatus: %v", err)
	}
}
