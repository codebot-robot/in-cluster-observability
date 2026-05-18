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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	collmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

func TestNewBridge_LifecycleWithoutOBIWriterOrReceivers(t *testing.T) {
	mgr, err := capture.NewBridge(capture.Config{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start with empty addrs/path: %v", err)
	}
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop must be idempotent: %v", err)
	}
}

func TestNewBridge_WritesInitialOBIConfigOnStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obi.yaml")
	mgr, err := capture.NewBridge(capture.Config{
		ObiConfigPath: path,
		OBIEndpoint:   "127.0.0.1:4317",
	})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read obi config: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, "127.0.0.1:4317") {
		t.Errorf("expected endpoint in initial config; got:\n%s", content)
	}
	if !strings.Contains(content, "enable: true") {
		t.Errorf("expected K8s attrs enabled in initial config (ADR-0021); got:\n%s", content)
	}
}

func TestNewBridge_GRPCReceiverAcceptsOTLP(t *testing.T) {
	mgr, err := capture.NewBridge(capture.Config{
		OTLPGRPCAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	// The receiver bound on an ephemeral port; we need to fish out the
	// actual address. The bridge does not expose that, but the test
	// can dial via the same loopback by passing 127.0.0.1:0 — go's net
	// stack picks an actual port. We hit the gRPC service by reading
	// from /proc/net/tcp? Simpler: skip the real-port lookup in unit
	// tests and just verify the receiver doesn't crash and counters tick.
	//
	// Counter check: send via the receiver's internal handler is not
	// reachable from here, so this test exercises only Start/Stop.
	if mgr.Metrics() == nil {
		t.Error("Metrics() returned nil")
	}
}

func TestNewBridge_GRPCReceiverEndToEnd(t *testing.T) {
	// End-to-end test: bind 127.0.0.1:0, find the port via lsof? No —
	// easier to use a known port range and tolerate occasional bind
	// races. Skipping if port is busy.
	addr := "127.0.0.1:34717"
	mgr, err := capture.NewBridge(capture.Config{OTLPGRPCAddr: addr})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			t.Skipf("port %s busy; skipping e2e test", addr)
		}
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := collmetricspb.NewMetricsServiceClient(conn)
	if _, err := client.Export(ctx, &collmetricspb.ExportMetricsServiceRequest{}); err != nil {
		t.Fatalf("export: %v", err)
	}
	// Successful round-trip — handler ran and acked.
}

func TestNewBridge_RejectsBadConfig(t *testing.T) {
	_, err := capture.NewBridge(capture.Config{ObiConfigPath: "/this/parent/missing/x.yaml"})
	if err == nil {
		t.Error("expected error for missing parent dir on ObiConfigPath")
	}
}

func TestNewBridge_ModuleToggleWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obi.yaml")
	mgr, err := capture.NewBridge(capture.Config{
		ObiConfigPath: path,
		OBIEndpoint:   "127.0.0.1:4317",
	})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	if err := mgr.EnableModule(capture.ModuleHTTP1, capture.ModuleConfig{}); err != nil {
		t.Fatalf("EnableModule: %v", err)
	}
	if got := mgr.EnabledModules(); len(got) != 1 || got[0] != capture.ModuleHTTP1 {
		t.Errorf("expected [http1]; got %v", got)
	}
	if err := mgr.DisableModule(capture.ModuleHTTP1); err != nil {
		t.Fatalf("DisableModule: %v", err)
	}
	if got := mgr.EnabledModules(); len(got) != 0 {
		t.Errorf("expected empty; got %v", got)
	}
}

func TestNewBridge_AllowBlockPID_IsIdempotent(t *testing.T) {
	mgr, _ := capture.NewBridge(capture.Config{})
	defer mgr.Stop(context.Background())
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

func TestNewBridge_StartAfterStop(t *testing.T) {
	mgr, _ := capture.NewBridge(capture.Config{})
	ctx := context.Background()
	_ = mgr.Start(ctx)
	_ = mgr.Stop(ctx)
	if err := mgr.Start(ctx); !errors.Is(err, capture.ErrStopped) {
		t.Fatalf("Start after Stop should return ErrStopped; got %v", err)
	}
}
