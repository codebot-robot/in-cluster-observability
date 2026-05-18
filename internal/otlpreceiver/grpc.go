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

package otlpreceiver

import (
	"context"

	colllogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

// registerGRPC wires the OTLP collector services on s. Each Export
// RPC delegates to h and returns the standard ExportXxxServiceResponse
// shape (empty response on success per the OTLP spec).
func registerGRPC(s *grpc.Server, h Handler) {
	collmetricspb.RegisterMetricsServiceServer(s, &grpcMetrics{h: h})
	colltracepb.RegisterTraceServiceServer(s, &grpcTraces{h: h})
	colllogspb.RegisterLogsServiceServer(s, &grpcLogs{h: h})
}

type grpcMetrics struct {
	collmetricspb.UnimplementedMetricsServiceServer
	h Handler
}

func (g *grpcMetrics) Export(ctx context.Context, req *collmetricspb.ExportMetricsServiceRequest) (*collmetricspb.ExportMetricsServiceResponse, error) {
	if err := g.h.OnMetrics(ctx, req); err != nil {
		return nil, err
	}
	return &collmetricspb.ExportMetricsServiceResponse{}, nil
}

type grpcTraces struct {
	colltracepb.UnimplementedTraceServiceServer
	h Handler
}

func (g *grpcTraces) Export(ctx context.Context, req *colltracepb.ExportTraceServiceRequest) (*colltracepb.ExportTraceServiceResponse, error) {
	if err := g.h.OnTraces(ctx, req); err != nil {
		return nil, err
	}
	return &colltracepb.ExportTraceServiceResponse{}, nil
}

type grpcLogs struct {
	colllogspb.UnimplementedLogsServiceServer
	h Handler
}

func (g *grpcLogs) Export(ctx context.Context, req *colllogspb.ExportLogsServiceRequest) (*colllogspb.ExportLogsServiceResponse, error) {
	if err := g.h.OnLogs(ctx, req); err != nil {
		return nil, err
	}
	return &colllogspb.ExportLogsServiceResponse{}, nil
}
