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

// Package query exposes the PromQL (metrics) and CEL (spans/edges)
// query engines per ADR-0008. v0.1 ships only the interface surface;
// concrete engines and fan-out land in v0.5
// (see docs/design/storage-and-query.md §4).
package query

// Engine is the consumer-facing query handle. Concrete methods
// (QueryInstant, QueryRange, QuerySpans, QueryEdges) fill in v0.5
// with their respective return types.
//
// Stability: Stable
type Engine any
