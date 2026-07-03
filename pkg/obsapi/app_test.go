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

package obsapi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gke-labs/in-cluster-observability/pkg/obsapi"
)

func TestNew_RejectsZeroRole(t *testing.T) {
	if _, err := obsapi.New(obsapi.Config{}); err == nil {
		t.Fatal("expected error when Role is zero")
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	app, err := obsapi.New(obsapi.Config{Role: obsapi.RoleAll})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := app.Config()
	if cfg.Namespace != "ollie-system" {
		t.Errorf("Namespace default; got %q", cfg.Namespace)
	}
	if cfg.StorePath != "/var/lib/ollie" {
		t.Errorf("StorePath default; got %q", cfg.StorePath)
	}
	if cfg.Retention != 10*time.Minute {
		t.Errorf("Retention default; got %v", cfg.Retention)
	}
}

func TestNew_ExposesSubsystems(t *testing.T) {
	app, err := obsapi.New(obsapi.Config{Role: obsapi.RoleAgent})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Capture() == nil {
		t.Error("Capture() must return a non-nil Manager (no-op is fine in v0.1)")
	}
	if app.Sinks() == nil {
		t.Error("Sinks() must return a non-nil Registry")
	}
	// store, query, topology may be nil in v0.1.
}

func TestRun_BlocksUntilContextDone(t *testing.T) {
	app, err := obsapi.New(obsapi.Config{Role: obsapi.RoleAgent})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v; want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of context cancel")
	}
}

func TestRole_String(t *testing.T) {
	for _, tc := range []struct {
		r    obsapi.Role
		want string
	}{
		{0, "none"},
		{obsapi.RoleAgent, "agent"},
		{obsapi.RoleController, "controller"},
		{obsapi.RoleQuery, "query"},
		{obsapi.RoleAgent | obsapi.RoleQuery, "agent,query"},
		{obsapi.RoleAll, "agent,controller,query"},
	} {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("Role(%d).String() = %q; want %q", tc.r, got, tc.want)
		}
	}
}
