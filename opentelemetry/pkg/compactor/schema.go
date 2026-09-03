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

package compactor

import (
	"encoding/json"
	"fmt"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// LogRow matches the ClickHouse-exporter schema for logs plus promoted k8s fields.
type LogRow struct {
	Timestamp          time.Time         `parquet:"Timestamp,timestamp(nanosecond)"`
	TraceId            string            `parquet:"TraceId"`
	SpanId             string            `parquet:"SpanId"`
	TraceFlags         uint32            `parquet:"TraceFlags"`
	SeverityText       string            `parquet:"SeverityText"`
	SeverityNumber     int32             `parquet:"SeverityNumber"`
	ServiceName        string            `parquet:"ServiceName"`
	K8sNamespace       string            `parquet:"K8sNamespace"`
	K8sPod             string            `parquet:"K8sPod"`
	Body               string            `parquet:"Body"`
	ResourceSchemaUrl  string            `parquet:"ResourceSchemaUrl"`
	ResourceAttributes map[string]string `parquet:"ResourceAttributes"`
	ScopeName          string            `parquet:"ScopeName"`
	ScopeVersion       string            `parquet:"ScopeVersion"`
	ScopeAttributes    map[string]string `parquet:"ScopeAttributes"`
	LogAttributes      map[string]string `parquet:"LogAttributes"`
}

// SpanEvent represents a nested trace event.
type SpanEvent struct {
	Timestamp  time.Time         `parquet:"Timestamp,timestamp(nanosecond)"`
	Name       string            `parquet:"Name"`
	Attributes map[string]string `parquet:"Attributes"`
}

// SpanLink represents a nested trace link.
type SpanLink struct {
	TraceId    string            `parquet:"TraceId"`
	SpanId     string            `parquet:"SpanId"`
	TraceState string            `parquet:"TraceState"`
	Attributes map[string]string `parquet:"Attributes"`
}

// TraceRow matches the ClickHouse-exporter schema for traces plus promoted k8s fields.
type TraceRow struct {
	Timestamp          time.Time         `parquet:"Timestamp,timestamp(nanosecond)"`
	Duration           int64             `parquet:"Duration"`
	TraceId            string            `parquet:"TraceId"`
	SpanId             string            `parquet:"SpanId"`
	ParentSpanId       string            `parquet:"ParentSpanId"`
	TraceState         string            `parquet:"TraceState"`
	SpanName           string            `parquet:"SpanName"`
	SpanKind           string            `parquet:"SpanKind"`
	ServiceName        string            `parquet:"ServiceName"`
	K8sNamespace       string            `parquet:"K8sNamespace"`
	K8sPod             string            `parquet:"K8sPod"`
	ResourceAttributes map[string]string `parquet:"ResourceAttributes"`
	ScopeName          string            `parquet:"ScopeName"`
	ScopeVersion       string            `parquet:"ScopeVersion"`
	SpanAttributes     map[string]string `parquet:"SpanAttributes"`
	StatusCode         string            `parquet:"StatusCode"`
	StatusMessage      string            `parquet:"StatusMessage"`
	Events             []SpanEvent       `parquet:"Events"`
	Links              []SpanLink        `parquet:"Links"`
}

// MetricRow matches the unified "points" table covering gauge + sum plus promoted k8s fields.
type MetricRow struct {
	Timestamp              time.Time         `parquet:"Timestamp,timestamp(nanosecond)"`
	StartTimestamp         time.Time         `parquet:"StartTimestamp,timestamp(nanosecond)"`
	MetricName             string            `parquet:"MetricName"`
	MetricDescription      string            `parquet:"MetricDescription"`
	MetricUnit             string            `parquet:"MetricUnit"`
	MetricType             string            `parquet:"MetricType"` // "gauge" or "sum"
	AggregationTemporality string            `parquet:"AggregationTemporality"`
	IsMonotonic            bool              `parquet:"IsMonotonic"`
	Value                  float64           `parquet:"Value"`
	Flags                  uint32            `parquet:"Flags"`
	ServiceName            string            `parquet:"ServiceName"`
	K8sNamespace           string            `parquet:"K8sNamespace"`
	K8sPod                 string            `parquet:"K8sPod"`
	ResourceAttributes     map[string]string `parquet:"ResourceAttributes"`
	ScopeName              string            `parquet:"ScopeName"`
	ScopeVersion           string            `parquet:"ScopeVersion"`
	Attributes             map[string]string `parquet:"Attributes"`
}

// AnyValueToString converts an OTLP AnyValue to its string representation.
func AnyValueToString(val *commonpb.AnyValue) string {
	if val == nil {
		return ""
	}
	switch v := val.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return v.StringValue
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("%t", v.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("%d", v.IntValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("%g", v.DoubleValue)
	case *commonpb.AnyValue_BytesValue:
		return string(v.BytesValue)
	case *commonpb.AnyValue_KvlistValue:
		b, err := json.Marshal(v.KvlistValue)
		if err != nil {
			return ""
		}
		return string(b)
	case *commonpb.AnyValue_ArrayValue:
		b, err := json.Marshal(v.ArrayValue)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		return ""
	}
}

// AttributesToMap converts an OTLP KeyValue slice into a map[string]string.
func AttributesToMap(attrs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		m[kv.Key] = AnyValueToString(kv.Value)
	}
	return m
}

// GetAttribute returns the string value of a specific attribute key.
func GetAttribute(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.Key == key {
			return AnyValueToString(kv.Value)
		}
	}
	return ""
}

// ExtractPromoted extracts ServiceName, K8sNamespace, and K8sPod from lists of attributes.
func ExtractPromoted(resourceAttrs, scopeAttrs, recordAttrs []*commonpb.KeyValue) (serviceName, k8sNamespace, k8sPod string) {
	// Look up in record attributes first (most specific), then scope, then resource
	k8sNamespace = GetAttribute(recordAttrs, "k8s.namespace.name")
	if k8sNamespace == "" {
		k8sNamespace = GetAttribute(scopeAttrs, "k8s.namespace.name")
	}
	if k8sNamespace == "" {
		k8sNamespace = GetAttribute(resourceAttrs, "k8s.namespace.name")
	}

	k8sPod = GetAttribute(recordAttrs, "k8s.pod.name")
	if k8sPod == "" {
		k8sPod = GetAttribute(scopeAttrs, "k8s.pod.name")
	}
	if k8sPod == "" {
		k8sPod = GetAttribute(resourceAttrs, "k8s.pod.name")
	}

	serviceName = GetAttribute(recordAttrs, "service.name")
	if serviceName == "" {
		serviceName = GetAttribute(scopeAttrs, "service.name")
	}
	if serviceName == "" {
		serviceName = GetAttribute(resourceAttrs, "service.name")
	}
	if serviceName == "" {
		serviceName = "unknown_service"
	}

	return
}
