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

import "time"

// Config governs App construction. Sensible defaults everywhere; the
// minimal valid Config is a non-zero Role.
//
// Stability: Stable
type Config struct {
	// Role selects which subsystems this process runs.
	Role Role
	// Namespace is the install namespace, used for in-cluster service
	// discovery (Controller endpoint, query-server Service, etc.).
	// Defaults to "ollie-system" when empty.
	Namespace string
	// StorePath is the on-disk root for the in-cluster store's WAL and
	// snapshots. Defaults to "/var/lib/ollie" when empty.
	StorePath string
	// Retention is how long the in-cluster store keeps data before
	// FIFO eviction. Defaults to 10 minutes when zero.
	Retention time.Duration
}

// Defaults returns a fully-populated copy of c with empty fields filled.
//
// Stability: Stable
func (c Config) Defaults() Config {
	out := c
	if out.Namespace == "" {
		out.Namespace = "ollie-system"
	}
	if out.StorePath == "" {
		out.StorePath = "/var/lib/ollie"
	}
	if out.Retention == 0 {
		out.Retention = 10 * time.Minute
	}
	return out
}
