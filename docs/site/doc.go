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

// Package site is the Hugo + Docsy documentation site for
// in-cluster-observability. The actual content lives under
// content/; this file exists only so that `go test ./...` (run
// by `ap test`) finds at least one Go package in the module —
// otherwise it errors with "matched no packages" because the
// only go.mod here is the Hugo module wrapper.
package site
