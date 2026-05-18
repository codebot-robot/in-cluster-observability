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

// Package v1alpha1 holds the v1alpha1 API types for Ollie's control
// plane CRDs: TrafficMonitor (namespaced) and ClusterTrafficPolicy
// (cluster-scoped). API group is `ollie.gke-labs.dev` per ADR-0022.1.
//
// +kubebuilder:object:generate=true
// +groupName=ollie.gke-labs.dev
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is the group + version for the v1alpha1 API.
var GroupVersion = schema.GroupVersion{Group: "ollie.gke-labs.dev", Version: "v1alpha1"}

// SchemeBuilder registers the v1alpha1 types with a runtime.Scheme.
// Use SchemeBuilder.AddToScheme from cmd/controller's main.go.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the v1alpha1 types to the given scheme.
var AddToScheme = SchemeBuilder.AddToScheme
