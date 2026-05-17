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

package obsapi

import "strings"

// Role is a bitmask selecting which subsystems an embedder runs. The
// default binary supports any combination; production deployments
// typically split agent / controller / query across pods.
//
// Stability: Stable
type Role uint32

const (
	// RoleAgent runs capture + local store + push sinks (DaemonSet).
	RoleAgent Role = 1 << iota
	// RoleController runs reconcilers, identity broadcaster, webhook.
	RoleController
	// RoleQuery runs the query server, pull/streaming sinks, custom-metrics-API.
	RoleQuery
)

// RoleAll combines the three production roles into a single process.
// Convenient for single-node dev or test environments.
//
// Stability: Stable
const RoleAll = RoleAgent | RoleController | RoleQuery

// Has reports whether the role bitmask includes r.
//
// Stability: Stable
func (r Role) Has(want Role) bool {
	return r&want != 0
}

// String returns a stable comma-separated rendering of the active
// roles, e.g. "agent,query". An unset Role renders as "none".
//
// Stability: Stable
func (r Role) String() string {
	if r == 0 {
		return "none"
	}
	var parts []string
	if r.Has(RoleAgent) {
		parts = append(parts, "agent")
	}
	if r.Has(RoleController) {
		parts = append(parts, "controller")
	}
	if r.Has(RoleQuery) {
		parts = append(parts, "query")
	}
	return strings.Join(parts, ",")
}
