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

// Command ollie-controller is the v0.4 control-plane binary. It runs
// as a 2-replica Deployment in the install namespace, elects a leader
// via a Lease, watches TrafficMonitor + ClusterTrafficPolicy + Pod,
// computes per-pod MonitoringSpecs, and streams them to agents via the
// gRPC AgentSession bidirectional stream. Per ADR-0022, no validating
// webhook in v0.4 and no identity broadcasting.
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

	"google.golang.org/grpc"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/gke-labs/in-cluster-observability/pkg/controller/api/v1alpha1"
	cppb "github.com/gke-labs/in-cluster-observability/pkg/controller/pb/controlplane/v1"
	"github.com/gke-labs/in-cluster-observability/pkg/controller/reconciler"
	"github.com/gke-labs/in-cluster-observability/pkg/controller/stream"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "v0.4.0-dev"

func main() {
	versionOnly := flag.Bool("version", false, "print version and exit")
	metricsAddr := flag.String("metrics-addr", ":9100", "bind address for the controller's /metrics endpoint (empty disables)")
	probeAddr := flag.String("probe-addr", ":9101", "bind address for /healthz + /readyz")
	streamAddr := flag.String("stream-addr", ":9102", "gRPC bind address for the agent AgentSession stream")
	leaderElection := flag.Bool("leader-elect", true, "enable Lease-based leader election (one replica accepts agent streams at a time)")
	leaderElectionID := flag.String("leader-election-id", "ollie-controller", "Lease name for leader election")
	leaderElectionNS := flag.String("leader-election-namespace", "", "namespace for the leader-election Lease; empty = in-cluster namespace")
	flag.Parse()

	if *versionOnly {
		fmt.Println(version)
		return
	}

	fmt.Fprintf(os.Stderr, "ollie-controller %s\n", version)
	fmt.Fprintln(os.Stderr, "v0.4 control plane: TrafficMonitor + ClusterTrafficPolicy → MonitoringSpec → agents (per ADR-0022)")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := ctrl.GetConfig()
	if err != nil {
		fatalf("get kubeconfig: %v\n", err)
	}

	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                        s,
		Metrics:                       metricsserver.Options{BindAddress: *metricsAddr},
		HealthProbeBindAddress:        *probeAddr,
		LeaderElection:                *leaderElection,
		LeaderElectionID:              *leaderElectionID,
		LeaderElectionNamespace:       *leaderElectionNS,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		fatalf("NewManager: %v\n", err)
	}

	dispatcher := stream.NewDispatcher()
	engine := &reconciler.Engine{Client: mgr.GetClient(), Dispatcher: dispatcher}

	if err := ctrl.NewControllerManagedBy(mgr).
		Named("trafficmonitor").
		For(&v1alpha1.TrafficMonitor{}).
		Complete(&reconciler.TrafficMonitorReconciler{Engine: engine}); err != nil {
		fatalf("setup TrafficMonitorReconciler: %v\n", err)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		Named("clustertrafficpolicy").
		For(&v1alpha1.ClusterTrafficPolicy{}).
		Complete(&reconciler.ClusterTrafficPolicyReconciler{Engine: engine}); err != nil {
		fatalf("setup ClusterTrafficPolicyReconciler: %v\n", err)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		Named("pod-trigger").
		For(&corev1.Pod{}).
		Complete(&reconciler.PodReconciler{Engine: engine}); err != nil {
		fatalf("setup PodReconciler: %v\n", err)
	}

	if err := mgr.AddHealthzCheck("alive", func(_ *http.Request) error { return nil }); err != nil {
		fatalf("AddHealthzCheck: %v\n", err)
	}
	if err := mgr.AddReadyzCheck("ready", func(_ *http.Request) error { return nil }); err != nil {
		fatalf("AddReadyzCheck: %v\n", err)
	}

	// gRPC AgentSession server. Only the leader accepts streams;
	// followers reject with FailedPrecondition so agents reconnect
	// to the new leader on failover. mgr.Elected() closes when this
	// replica wins the lease.
	grpcServer := grpc.NewServer()
	cppb.RegisterControlPlaneServer(grpcServer, &stream.Server{
		Dispatcher: dispatcher,
		IsLeader: func() bool {
			select {
			case <-mgr.Elected():
				return true
			default:
				return false
			}
		},
	})
	lis, err := net.Listen("tcp", *streamAddr)
	if err != nil {
		fatalf("stream listen %s: %v\n", *streamAddr, err)
	}
	go func() {
		fmt.Fprintf(os.Stderr, "gRPC stream server: listening on %s\n", lis.Addr())
		if err := grpcServer.Serve(lis); err != nil {
			fmt.Fprintf(os.Stderr, "gRPC stream server: %v\n", err)
		}
	}()
	defer grpcServer.GracefulStop()

	if err := mgr.Start(ctx); err != nil {
		fatalf("manager.Start: %v\n", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
