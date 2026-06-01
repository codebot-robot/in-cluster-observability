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
//
// v0.2 wired the OBI-sibling-container capture pipeline: the agent
// starts an OTLP receiver on loopback (consuming from the sibling
// OBI container) and an OBI config writer (under a shared volume).
//
// v0.3 (per ADR-0021) keeps the agent thin: OBI does K8s metadata
// enrichment via its informer, and the agent is the OBI config writer
// + OTLP receiver carve-out hook point for v0.4 (controller-driven
// filtering) and v0.5 (in-cluster store + query). Until the v0.4
// controller exists, --obi-instrument-ports seeds OBI's discovery so
// Application mode has something to attach to.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/gke-labs/in-cluster-observability/internal/debugendpoint"
	"github.com/gke-labs/in-cluster-observability/pkg/capture"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "v0.4.0-dev"

func main() {
	versionOnly := flag.Bool("version", false, "print version and exit")

	// v0.2 capture flags.
	otlpGRPC := flag.String("otlp-grpc-addr", "127.0.0.1:4317", "loopback bind address for OTLP gRPC receiver (consumed from sibling OBI container)")
	otlpHTTP := flag.String("otlp-http-addr", "127.0.0.1:4318", "loopback bind address for OTLP HTTP receiver")
	obiConfig := flag.String("obi-config", "/etc/ollie/obi-config/config.yaml", "shared-volume path where the agent writes OBI's config (empty disables writing)")
	obiInstrumentPorts := flag.String("obi-instrument-ports", "", "seed OBI's discovery.instrument with one entry matching processes on these listening ports (OBI format: \"80\", \"80,8080\", \"8000-8999\"). v0.3 L7 smoke-test knob; harmless once the v0.4 controller pushes per-PID MonitoringSpecs.")
	scrapeAddr := flag.String("scrape-addr", "0.0.0.0:9090", "bind address for the production Prometheus scrape endpoint at /metrics (empty disables). Per ADR-0021 this is the single scrape URL — exposes both agent self-obs and re-emitted OBI metrics.")
	controllerAddr := flag.String("controller-addr", "", "gRPC target for the v0.4 ollie-controller (e.g. ollie-controller.ollie-system.svc:9102). Empty disables the controller client; agent runs in standalone v0.3 mode (--obi-instrument-ports seed).")
	nodeName := flag.String("node-name", os.Getenv("KUBE_NODE_NAME"), "K8s node this agent runs on. Defaults to $KUBE_NODE_NAME (populated via Downward API in k8s/daemonset.yaml).")
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
	fmt.Fprintln(os.Stderr, "v0.3: OTLP receiver + OBI config writer; OBI does K8s enrichment (per ADR-0021)")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// One MeterProvider feeds everything: agent self-obs counters
	// (ollie_capture_*) and the metric forwarder that re-emits OBI's
	// translated metrics. The same Prometheus handler backs the
	// production scrape listener on :9090 and the optional
	// /debug/metrics on the loopback debug endpoint.
	mp, promHandler, err := capture.NewPromMeterProvider()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prometheus exporter init failed: %v\n", err)
		os.Exit(1)
	}

	// Always-on "agent is alive and scrape path is wired" signal.
	// The OTel SDK only materializes a metric stream on first Add/
	// Record, so a freshly-started agent with no traffic produces an
	// empty /metrics aside from target_info. This gauge gives
	// scrapers a non-empty signal from boot.
	if up, err := mp.Meter("ollie/agent").Float64Gauge("ollie_agent_up",
		metric.WithDescription("1 if the ollie agent is running and its scrape path is wired"),
	); err == nil {
		up.Record(ctx, 1, metric.WithAttributes(
			attribute.String("version", version),
		))
	}

	captureCfg := capture.Config{
		OTLPGRPCAddr:     *otlpGRPC,
		OTLPHTTPAddr:     *otlpHTTP,
		ObiConfigPath:    *obiConfig,
		InitialOpenPorts: *obiInstrumentPorts,
		MeterProvider:    mp,
	}

	mgr, err := capture.NewBridge(captureCfg)
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
	if *obiInstrumentPorts != "" {
		fmt.Fprintf(os.Stderr, "OBI smoke-test discovery seeded: open_ports=%s\n", *obiInstrumentPorts)
	}

	// Production Prometheus scrape listener.
	var scrapeServer *http.Server
	if *scrapeAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promHandler)
		l, err := net.Listen("tcp", *scrapeAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scrape listen %s: %v\n", *scrapeAddr, err)
			os.Exit(1)
		}
		scrapeServer = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := scrapeServer.Serve(l); err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "scrape server: %v\n", err)
			}
		}()
		defer func() {
			sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer sCancel()
			_ = scrapeServer.Shutdown(sCtx)
		}()
		fmt.Fprintf(os.Stderr, "scrape endpoint: http://%s/metrics\n", l.Addr())
	}

	traceStore := debugendpoint.NewTraceStore(5000)

	// Loopback debug endpoint (off by default).
	if *debugEnable {
		opts := []debugendpoint.Option{
			debugendpoint.WithExtraHandler("GET /debug/metrics", promHandler),
			debugendpoint.WithTraceStore(traceStore),
		}
		dbg, err := debugendpoint.New(mgr, *debugAddr, opts...)
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
		fmt.Fprintf(os.Stderr, "debug endpoint enabled on %s (loopback); /debug/metrics serves agent self-obs\n", actualAddr)
		fmt.Fprintf(os.Stderr, "debug endpoint: http://%s/debug/explorer serves HTML Trace Explorer\n", actualAddr)
	}

	// Forwarder + writer: re-emit each MetricEvent into the OTel SDK
	// Meter so OBI's translated metrics flow out via the same
	// Prometheus exporter the scrape listener serves. SpanEvents are stored in the traceStore.
	fwd := newMetricForwarder(mp.Meter("ollie/obi-forwarder"))
	go func() {
		for ev := range mgr.Events() {
			switch ev.Kind {
			case capture.EventMetric:
				if ev.Metric != nil {
					fwd.Record(ctx, *ev.Metric)
				}
			case capture.EventSpan:
				if ev.Span != nil {
					traceStore.Add(ev.Span)
				}
			}
		}
	}()

	// v0.4 controller client. Opt-in via --controller-addr. When
	// set, the agent connects to the controller's gRPC AgentSession
	// stream and applies received MonitoringSpec deltas via
	// capture.Manager.AllowPID / BlockPID. When unset, the agent
	// runs in standalone v0.3 mode (--obi-instrument-ports seed).
	if *controllerAddr != "" {
		if *nodeName == "" {
			fmt.Fprintln(os.Stderr, "controller client requires --node-name (or $KUBE_NODE_NAME); aborting")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "controller client: connecting to %s as node=%s\n", *controllerAddr, *nodeName)
		go runControllerClient(ctx, *controllerAddr, *nodeName, mgr, func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		})
	} else {
		fmt.Fprintln(os.Stderr, "controller client: disabled (--controller-addr empty); standalone v0.3 mode")
	}

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "received shutdown signal; draining")
}
