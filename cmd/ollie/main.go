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

// Command ollie is the default binary that ships with the project.
// v0.2 wires the OBI-sibling-container capture pipeline: the agent
// starts an OTLP receiver on loopback (consuming from the sibling
// OBI container), an OBI config writer (under a shared volume), and
// — optionally — a loopback-only debug HTTP endpoint for manual PID
// control.
//
// Real CRD-driven control lands with the controller in v0.4; until
// then operators drive AllowPID via the debug endpoint when needed
// (per ADR-0017.3).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gke-labs/in-cluster-observability/internal/debugendpoint"
	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "v0.2.0-dev"

func main() {
	versionOnly := flag.Bool("version", false, "print version and exit")

	// v0.2 capture flags.
	otlpGRPC := flag.String("otlp-grpc-addr", "127.0.0.1:4317", "loopback bind address for OTLP gRPC receiver (consumed from sibling OBI container)")
	otlpHTTP := flag.String("otlp-http-addr", "127.0.0.1:4318", "loopback bind address for OTLP HTTP receiver")
	obiConfig := flag.String("obi-config", "/etc/ollie/obi-config/config.yaml", "shared-volume path where the agent writes OBI's config (empty disables writing)")
	debugEnable := flag.Bool("debug-endpoint", false, "enable the loopback debug HTTP endpoint on 127.0.0.1:9099 (off by default per ADR-0017.3)")
	debugAddr := flag.String("debug-endpoint-addr", debugendpoint.DefaultAddr, "loopback bind address for the debug endpoint")

	// v0.1 compatibility: stay-alive made sense when there was no real
	// work to do. v0.2's agent always blocks on signals; the flag is
	// kept for backward compat but is a no-op now.
	_ = flag.Bool("stay-alive", false, "deprecated in v0.2; agent always blocks on signals")
	flag.Parse()

	if *versionOnly {
		fmt.Println(version)
		return
	}

	fmt.Fprintf(os.Stderr, "ollie %s\n", version)
	fmt.Fprintln(os.Stderr, "v0.2 Capture MVP: starting OTLP receiver + OBI config writer (per ADR-0018)")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mgr, err := capture.NewBridge(capture.Config{
		OTLPGRPCAddr:  *otlpGRPC,
		OTLPHTTPAddr:  *otlpHTTP,
		ObiConfigPath: *obiConfig,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture init failed: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "capture start failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithCancel(context.Background())
		defer stopCancel()
		_ = mgr.Stop(stopCtx)
	}()
	fmt.Fprintf(os.Stderr, "OTLP receiver: gRPC=%s HTTP=%s; OBI config: %s\n", *otlpGRPC, *otlpHTTP, *obiConfig)

	if *debugEnable {
		dbg, err := debugendpoint.New(mgr, *debugAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "debug endpoint init failed: %v\n", err)
			os.Exit(1)
		}
		actualAddr, err := dbg.Start(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "debug endpoint start failed: %v\n", err)
			os.Exit(1)
		}
		defer dbg.Stop(context.Background())
		fmt.Fprintf(os.Stderr, "debug endpoint enabled on %s (loopback)\n", actualAddr)
	}

	// Drain Events() in a goroutine so the channel never backs up.
	// v0.2 doesn't have an enricher / store / sinks yet; events are
	// silently consumed (counted via ollie_capture_events_total).
	go func() {
		for range mgr.Events() {
		}
	}()

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "received shutdown signal; draining")
}
