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

// Package obiconfig models the subset of OpenTelemetry eBPF
// Instrumentation (OBI) configuration that the agent controls via the
// shared config volume. Per ADR-0018, OBI runs as a sibling container
// and watches this file; the agent writes it on MonitoringSpec changes
// and OBI reloads.
//
// The schema mirrors OBI's YAML config keys for the fields we set. It
// is intentionally a subset — operators retain control over OBI's other
// knobs via the install-time base config that this file overlays (TBD
// in v0.4 when the controller takes over). v0.2 writes a complete file
// each time; merge semantics with a base config arrive later.
package obiconfig

// File is the on-disk OBI config the agent writes. Marshals to YAML
// via gopkg.in/yaml.v3. Field names follow OBI's config schema.
type File struct {
	OtelMetricsExport OTLPExport `yaml:"otel_metrics_export"`
	OtelTracesExport  OTLPExport `yaml:"otel_traces_export"`
	Attributes        Attributes `yaml:"attributes"`
	Routes            *Routes    `yaml:"routes,omitempty"`
	Discovery         Discovery  `yaml:"discovery"`
}

// OTLPExport tells OBI where to send the corresponding telemetry. For
// the sibling-container model, both endpoints point at our agent's
// loopback OTLP receiver.
type OTLPExport struct {
	Endpoint string `yaml:"endpoint"`
}

// Attributes controls OBI's attribute attachment behavior.
type Attributes struct {
	Kubernetes KubernetesAttrs `yaml:"kubernetes"`
}

// KubernetesAttrs governs OBI's built-in K8s metadata attachment. Per
// ADR-0017.4 + ADR-0018, we disable this and re-attribute via
// pkg/topology in v0.3.
type KubernetesAttrs struct {
	Enable bool `yaml:"enable"`
}

// Routes governs OBI's URL-path handling. We leave templating to v0.6
// (#108) and set unmatched: wildcard so OBI doesn't drop paths.
type Routes struct {
	Unmatched string `yaml:"unmatched,omitempty"`
}

// Discovery is the per-target instrumentation selector list.
type Discovery struct {
	Services []Service `yaml:"services,omitempty"`
}

// Service is OBI's discovery selector for an instrumented workload.
// v0.2 fills in just enough fields for per-PID selection driven by
// AllowPID; richer selectors (label/namespace) arrive when the
// controller takes over in v0.4.
type Service struct {
	Name      string   `yaml:"name"`
	Namespace string   `yaml:"namespace,omitempty"`
	OpenPorts []uint16 `yaml:"open_ports,omitempty"`

	// PIDs is the explicit PID list this service matches. OBI's actual
	// discovery schema uses richer selectors; for v0.2 we model PID-set
	// targeting as a virtual selector that the writer translates as
	// needed (see writer.go). When the schema is verified against a
	// real OBI image, this field's mapping may change.
	PIDs []uint32 `yaml:"-"`
}

// DefaultFile returns a baseline File with the loopback OTLP endpoints
// configured and Kubernetes attribute attachment disabled.
//
// Stability: Experimental
func DefaultFile(otlpEndpoint string) File {
	return File{
		OtelMetricsExport: OTLPExport{Endpoint: otlpEndpoint},
		OtelTracesExport:  OTLPExport{Endpoint: otlpEndpoint},
		Attributes:        Attributes{Kubernetes: KubernetesAttrs{Enable: false}},
		Routes:            &Routes{Unmatched: "wildcard"},
		Discovery:         Discovery{Services: nil},
	}
}
