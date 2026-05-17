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
	"fmt"
	"net"
)

// requireLoopback rejects addresses that resolve to anything other
// than a loopback IP. Empty addr returns nil (caller decides to disable).
// Per ADR-0018, OTLP receivers bind loopback only — the OBI sibling
// container is on the same pod network namespace.
func requireLoopback(addr string) error {
	if addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("split host/port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("bind address %q has no host part; loopback (127.0.0.1, ::1) required", addr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// hostname — allow only "localhost"
		if host == "localhost" {
			return nil
		}
		return fmt.Errorf("bind host %q must be 127.0.0.1, ::1, or localhost", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("bind host %q is not a loopback address", host)
	}
	return nil
}
