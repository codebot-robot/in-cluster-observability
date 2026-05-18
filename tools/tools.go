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

//go:build tools

// Package tools blank-imports the code generators the project depends
// on so they are tracked in go.mod / go.sum and the right versions
// are resolved when `dev/scripts/codegen.sh` invokes them via
// `go run`. None of the imports here are referenced at runtime; the
// `tools` build tag keeps them out of normal builds.
//
// See https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	// controller-gen — CRD YAML + deepcopy + RBAC YAML from
	// +kubebuilder: markers (ADR-0022.3).
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"

	// protoc-gen-go / protoc-gen-go-grpc — Go message + service
	// stubs for proto/controlplane/v1/controlplane.proto.
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
