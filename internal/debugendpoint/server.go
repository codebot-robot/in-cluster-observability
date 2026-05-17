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

// Package debugendpoint is the developer-facing HTTP endpoint the
// agent exposes for manual PID control until the controller (v0.4)
// takes over. Per ADR-0017.3 the listener binds loopback only and is
// off by default; gated behind a --debug-endpoint flag on the agent.
//
// No authentication: loopback-only access requires `kubectl exec` into
// the agent pod, which already implies sufficient cluster privilege.
package debugendpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

// DefaultAddr is the canonical bind address: loopback, port 9099.
const DefaultAddr = "127.0.0.1:9099"

// Server hosts the debug endpoints over Manager.
//
// Stability: Experimental
type Server struct {
	mgr    capture.Manager
	addr   string
	srv    *http.Server
	listen net.Listener
}

// New constructs a debug Server bound to addr. addr must be loopback;
// non-loopback bind addresses are rejected (per ADR-0017.3).
//
// Stability: Experimental
func New(mgr capture.Manager, addr string) (*Server, error) {
	if mgr == nil {
		return nil, errors.New("debugendpoint: capture.Manager required")
	}
	if addr == "" {
		addr = DefaultAddr
	}
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	return &Server{mgr: mgr, addr: addr}, nil
}

// Start binds and serves in a background goroutine. Returns the
// actual bound address (useful for ephemeral-port tests).
func (s *Server) Start(ctx context.Context) (string, error) {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("debugendpoint: listen %q: %w", s.addr, err)
	}
	s.listen = l

	mux := http.NewServeMux()
	mux.HandleFunc("POST /debug/allow-pid", s.handleAllowPID)
	mux.HandleFunc("POST /debug/block-pid", s.handleBlockPID)
	mux.HandleFunc("GET /debug/state", s.handleState)

	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.srv.Serve(l)
	}()
	return l.Addr().String(), nil
}

// Stop drains and shuts the listener. Idempotent.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

type allowReq struct {
	PID     uint32            `json:"pid"`
	Modules []capture.Module  `json:"modules,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type blockReq struct {
	PID uint32 `json:"pid"`
}

type stateResp struct {
	PIDs    []uint32         `json:"pids"`
	Modules []capture.Module `json:"modules"`
}

func (s *Server) handleAllowPID(w http.ResponseWriter, r *http.Request) {
	var req allowReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.PID == 0 {
		http.Error(w, "pid must be non-zero", http.StatusBadRequest)
		return
	}
	spec := capture.PIDSpec{Protocols: req.Modules, Labels: req.Labels}
	if err := s.mgr.AllowPID(req.PID, spec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBlockPID(w http.ResponseWriter, r *http.Request) {
	var req blockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.PID == 0 {
		http.Error(w, "pid must be non-zero", http.StatusBadRequest)
		return
	}
	if err := s.mgr.BlockPID(req.PID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	// Manager doesn't expose its PID set directly; the debug endpoint
	// reports the enabled modules and an empty PID list when not
	// accessible via a public method. v0.3 will likely add a State()
	// method to capture.Manager; for v0.2 the endpoint is best-effort.
	resp := stateResp{
		Modules: s.mgr.EnabledModules(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
