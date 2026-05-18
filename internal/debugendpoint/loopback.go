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

package debugendpoint

import (
	"fmt"
	"net"
)

// requireLoopback rejects non-loopback bind addresses per ADR-0017.3.
// Mirrors the policy in internal/otlpreceiver.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("debugendpoint: split host/port: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("debugendpoint: host %q must be 127.0.0.1, ::1, or localhost", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("debugendpoint: host %q is not a loopback address", host)
	}
	return nil
}
