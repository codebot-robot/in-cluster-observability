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

package reconciler_test

import (
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/gke-labs/in-cluster-observability/pkg/controller/api/v1alpha1"
	"github.com/gke-labs/in-cluster-observability/pkg/controller/reconciler"
)

// TestComputeCoverage_BasicRollup confirms one CR + one matching pod
// gives a Specs entry, a CoveredPods entry, and no conflicts.
func TestComputeCoverage_BasicRollup(t *testing.T) {
	p := pod("u1", "nginx-1", "demo", "node-a", map[string]string{"app": "nginx"})
	t1 := tm("nginx-monitor", "demo", map[string]string{"app": "nginx"}, http1Core(8080))

	cov := reconciler.ComputeCoverage([]*corev1.Pod{p}, []*v1alpha1.TrafficMonitor{t1}, nil)
	if _, ok := cov.Specs[string(p.UID)]; !ok {
		t.Errorf("Specs missing entry for pod %s", p.UID)
	}
	st := cov.TMStatus[types.NamespacedName{Namespace: "demo", Name: "nginx-monitor"}]
	if st == nil {
		t.Fatal("TMStatus missing entry")
	}
	if len(st.MatchedPods) != 1 {
		t.Errorf("MatchedPods = %d; want 1", len(st.MatchedPods))
	}
	if len(st.CoveredPods) != 1 {
		t.Errorf("CoveredPods = %d; want 1", len(st.CoveredPods))
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("Conflicts = %v; want empty", st.Conflicts)
	}
}

// TestComputeCoverage_ConflictDetection: two TMs select the same
// pod with disagreeing protocol settings → both gain Conflict
// entries pointing at each other.
func TestComputeCoverage_ConflictDetection(t *testing.T) {
	p := pod("u2", "nginx-2", "demo", "node-a", map[string]string{"app": "nginx"})
	t1 := tm("aaaa", "demo", map[string]string{"app": "nginx"}, http1Core(8080))
	t2 := tm("zzzz", "demo", map[string]string{"app": "nginx"}, http1Core(9090))

	cov := reconciler.ComputeCoverage([]*corev1.Pod{p}, []*v1alpha1.TrafficMonitor{t1, t2}, nil)
	a := cov.TMStatus[types.NamespacedName{Namespace: "demo", Name: "aaaa"}]
	z := cov.TMStatus[types.NamespacedName{Namespace: "demo", Name: "zzzz"}]
	if a == nil || z == nil {
		t.Fatal("missing TMStatus entries")
	}
	wantA := []string{"demo/zzzz"}
	wantZ := []string{"demo/aaaa"}
	sort.Strings(a.Conflicts)
	sort.Strings(z.Conflicts)
	if !stringSliceEq(a.Conflicts, wantA) {
		t.Errorf("aaaa.Conflicts = %v; want %v", a.Conflicts, wantA)
	}
	if !stringSliceEq(z.Conflicts, wantZ) {
		t.Errorf("zzzz.Conflicts = %v; want %v", z.Conflicts, wantZ)
	}
	// aaaa wins coverage (lex-first); zzzz does not.
	if len(a.CoveredPods) != 1 {
		t.Errorf("aaaa.CoveredPods = %d; want 1 (lex-first wins)", len(a.CoveredPods))
	}
	if len(z.CoveredPods) != 0 {
		t.Errorf("zzzz.CoveredPods = %d; want 0", len(z.CoveredPods))
	}
}

// TestComputeCoverage_AgreeingSpecsAreNotConflicts: two TMs that
// produce byte-identical protocol settings on the same pod are NOT
// flagged as a conflict (one of them "wins" coverage; both still
// match the pod, but agreement means no operator-actionable
// problem).
func TestComputeCoverage_AgreeingSpecsAreNotConflicts(t *testing.T) {
	p := pod("u3", "nginx-3", "demo", "node-a", map[string]string{"app": "nginx"})
	t1 := tm("aaaa", "demo", map[string]string{"app": "nginx"}, http1Core(80))
	t2 := tm("bbbb", "demo", map[string]string{"app": "nginx"}, http1Core(80))

	cov := reconciler.ComputeCoverage([]*corev1.Pod{p}, []*v1alpha1.TrafficMonitor{t1, t2}, nil)
	for _, st := range cov.TMStatus {
		if len(st.Conflicts) != 0 {
			t.Errorf("Conflicts = %v; want empty (specs agree)", st.Conflicts)
		}
	}
}

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
