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

// Package topology defines Kubernetes-identity types and the Resolver
// interface used to attach K8s metadata to captured records. The
// concrete cache implementations land in v0.3 (source side) and v0.5
// (peer side, identity broadcast). See docs/design/topology.md.
package topology

import "net"

// Kind enumerates what a resolved Identity refers to.
//
// Stability: Stable
type Kind uint8

const (
	// KindUnknown is the zero value, signaling no resolution.
	KindUnknown Kind = iota
	// KindPod identifies a single Pod by namespace + name.
	KindPod
	// KindService identifies a Kubernetes Service by namespace + name.
	KindService
	// KindNode identifies a Kubernetes Node by name (namespace empty).
	KindNode
	// KindExternalIP indicates the peer IP did not resolve to any
	// in-cluster K8s object.
	KindExternalIP
)

// String returns a stable lowercase rendering of the Kind, suitable
// for attribute values.
//
// Stability: Stable
func (k Kind) String() string {
	switch k {
	case KindPod:
		return "pod"
	case KindService:
		return "service"
	case KindNode:
		return "node"
	case KindExternalIP:
		return "external_ip"
	default:
		return "unknown"
	}
}

// Identity describes a resolved K8s object (or external endpoint) at
// one end of a captured connection. It is the unit of source- and
// peer-side attribution.
//
// Stability: Stable
type Identity struct {
	Kind      Kind
	Namespace string
	Name      string
	Labels    map[string]string
	// Owner is the workload owner resolved one hop above the Pod
	// (Deployment, StatefulSet, DaemonSet, Job). Nil for non-Pod kinds
	// or when no owner is resolvable.
	Owner *OwnerRef
}

// OwnerRef names a workload owner (a parent of the Pod) without
// pulling in apimachinery types.
//
// Stability: Stable
type OwnerRef struct {
	Kind string // "Deployment" | "StatefulSet" | "DaemonSet" | "Job"
	Name string
}

// Resolver maps a peer IP (and optionally a port for service disambig)
// to a topology.Identity. The implementation lives in package internals
// and consults both a local PID cache and a cluster-wide IP cache.
//
// Stability: Stable
type Resolver interface {
	Lookup(ip net.IP) (Identity, bool)
	LookupWithPort(ip net.IP, port uint16) (Identity, bool)
}
