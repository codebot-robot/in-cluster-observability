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

// Package controller hosts the TrafficMonitor / ClusterTrafficPolicy
// reconcilers and the controller↔agent gRPC stream server. Concrete
// reconciler + stream implementations land in v0.4 Phase 2 (see
// docs/design/control-plane.md and ADR-0022). v0.4 Phase 1 ships:
//
//   - api/v1alpha1/  — CRD Go types with +kubebuilder: markers.
//   - pb/            — generated gRPC service stubs from
//     proto/controlplane/v1/controlplane.proto.
//
// Identity broadcasting (an earlier scope item per ADR-0009) is cut
// from v0.4 per ADR-0022.5; the validating admission webhook is
// deferred from v0.4 to v0.5 per ADR-0022.4.
package controller
