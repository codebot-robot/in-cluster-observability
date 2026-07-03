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

package capture_test

import (
	"errors"
	"testing"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

func TestNew_LifecycleNoop(t *testing.T) {
	mgr, err := capture.New(capture.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := t.Context()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop must be idempotent: %v", err)
	}
	if err := mgr.Start(ctx); !errors.Is(err, capture.ErrStopped) {
		t.Fatalf("Start after Stop must return ErrStopped; got %v", err)
	}
}

func TestNew_PIDLifecycleIsIdempotent(t *testing.T) {
	mgr, _ := capture.New(capture.Config{})
	defer mgr.Stop(t.Context())
	spec := capture.PIDSpec{Protocols: []capture.Module{capture.ModuleL4TCP}}
	if err := mgr.AllowPID(42, spec); err != nil {
		t.Fatalf("AllowPID: %v", err)
	}
	if err := mgr.AllowPID(42, spec); err != nil {
		t.Fatalf("repeat AllowPID: %v", err)
	}
	if err := mgr.BlockPID(42); err != nil {
		t.Fatalf("BlockPID: %v", err)
	}
	if err := mgr.BlockPID(42); err != nil {
		t.Fatalf("repeat BlockPID: %v", err)
	}
}

func TestNew_ModulesToggle(t *testing.T) {
	mgr, _ := capture.New(capture.Config{})
	defer mgr.Stop(t.Context())
	if got := mgr.EnabledModules(); len(got) != 0 {
		t.Fatalf("empty manager; got %d modules", len(got))
	}
	if err := mgr.EnableModule(capture.ModuleHTTP1, capture.ModuleConfig{}); err != nil {
		t.Fatalf("EnableModule: %v", err)
	}
	if got := mgr.EnabledModules(); len(got) != 1 || got[0] != capture.ModuleHTTP1 {
		t.Fatalf("expected [http1]; got %v", got)
	}
	if err := mgr.DisableModule(capture.ModuleHTTP1); err != nil {
		t.Fatalf("DisableModule: %v", err)
	}
	if got := mgr.EnabledModules(); len(got) != 0 {
		t.Fatalf("expected empty after disable; got %v", got)
	}
}

func TestNew_EventsClosedOnStop(t *testing.T) {
	mgr, _ := capture.New(capture.Config{})
	ctx := t.Context()
	_ = mgr.Start(ctx)
	ch := mgr.Events()
	_ = mgr.Stop(ctx)
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after Stop")
		}
	default:
		t.Fatal("Events channel should be readable (closed) after Stop")
	}
}

func TestModule_String(t *testing.T) {
	for _, tc := range []struct {
		m    capture.Module
		want string
	}{
		{capture.ModuleL4TCP, "l4_tcp"},
		{capture.ModuleHTTP1, "http1"},
		{capture.ModuleHTTP2, "http2"},
		{capture.ModuleGRPC, "grpc"},
		{capture.ModuleGenAI, "genai"},
		{capture.Module(999), "unknown"},
	} {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("Module(%d).String() = %q; want %q", tc.m, got, tc.want)
		}
	}
}
