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

package otlpreceiver_test

import (
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	colllogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/gke-labs/in-cluster-observability/internal/otlpreceiver"
)

type countHandler struct {
	metrics, traces, logs atomic.Int64
}

func (c *countHandler) OnMetrics(_ context.Context, _ *collmetricspb.ExportMetricsServiceRequest) error {
	c.metrics.Add(1)
	return nil
}
func (c *countHandler) OnTraces(_ context.Context, _ *colltracepb.ExportTraceServiceRequest) error {
	c.traces.Add(1)
	return nil
}
func (c *countHandler) OnLogs(_ context.Context, _ *colllogspb.ExportLogsServiceRequest) error {
	c.logs.Add(1)
	return nil
}

func TestNew_RejectsBadConfig(t *testing.T) {
	if _, err := otlpreceiver.New(otlpreceiver.Config{}); err == nil {
		t.Error("empty config should error")
	}
	if _, err := otlpreceiver.New(otlpreceiver.Config{Handler: &countHandler{}}); err == nil {
		t.Error("config with no addresses should error")
	}
	if _, err := otlpreceiver.New(otlpreceiver.Config{
		GRPCAddr: "1.2.3.4:4317", Handler: &countHandler{},
	}); err == nil {
		t.Error("non-loopback bind should error")
	}
	if _, err := otlpreceiver.New(otlpreceiver.Config{
		HTTPAddr: "0.0.0.0:4318", Handler: &countHandler{},
	}); err == nil {
		t.Error("0.0.0.0 bind should error")
	}
}

func TestServer_GRPCReceivesAllThreeSignals(t *testing.T) {
	h := &countHandler{}
	s, err := otlpreceiver.New(otlpreceiver.Config{
		GRPCAddr: "127.0.0.1:0",
		Handler:  h,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(t.Context())

	grpcAddr, _ := s.Addrs()
	if grpcAddr == "" {
		t.Fatal("Server.Addrs returned empty gRPC addr after Start")
	}

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	mc := collmetricspb.NewMetricsServiceClient(conn)
	tc := colltracepb.NewTraceServiceClient(conn)
	lc := colllogspb.NewLogsServiceClient(conn)
	if _, err := mc.Export(ctx, &collmetricspb.ExportMetricsServiceRequest{}); err != nil {
		t.Fatalf("metrics export: %v", err)
	}
	if _, err := tc.Export(ctx, &colltracepb.ExportTraceServiceRequest{}); err != nil {
		t.Fatalf("traces export: %v", err)
	}
	if _, err := lc.Export(ctx, &colllogspb.ExportLogsServiceRequest{}); err != nil {
		t.Fatalf("logs export: %v", err)
	}

	if got := h.metrics.Load(); got != 1 {
		t.Errorf("metrics handler hit count = %d; want 1", got)
	}
	if got := h.traces.Load(); got != 1 {
		t.Errorf("traces handler hit count = %d; want 1", got)
	}
	if got := h.logs.Load(); got != 1 {
		t.Errorf("logs handler hit count = %d; want 1", got)
	}
}

func TestServer_HTTPReceivesProtobufAndJSON(t *testing.T) {
	h := &countHandler{}
	s, err := otlpreceiver.New(otlpreceiver.Config{
		HTTPAddr: "127.0.0.1:0",
		Handler:  h,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(t.Context())

	_, httpAddr := s.Addrs()

	// protobuf
	body, err := proto.Marshal(&collmetricspb.ExportMetricsServiceRequest{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+httpAddr+"/v1/metrics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("protobuf POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("protobuf status = %d", resp.StatusCode)
	}

	// JSON
	jreq, _ := http.NewRequestWithContext(ctx, "POST", "http://"+httpAddr+"/v1/traces", bytes.NewReader([]byte("{}")))
	jreq.Header.Set("Content-Type", "application/json")
	jresp, err := http.DefaultClient.Do(jreq)
	if err != nil {
		t.Fatalf("json POST: %v", err)
	}
	jresp.Body.Close()
	if jresp.StatusCode != http.StatusOK {
		t.Errorf("json status = %d", jresp.StatusCode)
	}

	if got := h.metrics.Load(); got != 1 {
		t.Errorf("metrics count = %d; want 1", got)
	}
	if got := h.traces.Load(); got != 1 {
		t.Errorf("traces count = %d; want 1", got)
	}
}

func TestServer_StopIdempotent(t *testing.T) {
	s, _ := otlpreceiver.New(otlpreceiver.Config{
		GRPCAddr: "127.0.0.1:0",
		Handler:  &countHandler{},
	})
	_ = s.Start(t.Context())
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop should be no-op: %v", err)
	}
}
