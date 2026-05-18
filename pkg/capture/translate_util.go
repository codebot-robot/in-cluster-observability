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

package capture

import "strings"

// stripK8sAttrs removes OBI's built-in Kubernetes attribution from an
// attribute map. Per ADR-0017.4 + ADR-0018, the agent re-attributes
// K8s identity via pkg/topology in v0.3; passing OBI's k8s.* through
// would create two sources of truth.
//
// Strips any attribute whose key starts with "k8s." plus the common
// service.* duplicates OBI also sets from K8s metadata.
func stripK8sAttrs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if strings.HasPrefix(k, "k8s.") {
			continue
		}
		// service.name / service.namespace / service.instance.id are
		// frequently derived from K8s by OBI; strip those too so v0.3's
		// enricher owns them.
		switch k {
		case "service.name", "service.namespace", "service.instance.id":
			continue
		}
		out[k] = v
	}
	return out
}

// mergeMaps merges b on top of a (b wins on key collisions). Returns
// a new map; neither input is mutated. Nil-safe.
func mergeMaps(a, b map[string]string) map[string]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
