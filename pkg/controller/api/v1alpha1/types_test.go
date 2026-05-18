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

package v1alpha1_test

import (
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	v1alpha1 "github.com/gke-labs/in-cluster-observability/pkg/controller/api/v1alpha1"
)

// TestGroupVersion pins the API group string. ADR-0022.1 — changing
// this would be a breaking change requiring an API version migration.
func TestGroupVersion(t *testing.T) {
	if got, want := v1alpha1.GroupVersion.Group, "ollie.gke-labs.dev"; got != want {
		t.Errorf("GroupVersion.Group = %q; want %q (per ADR-0022.1)", got, want)
	}
	if got, want := v1alpha1.GroupVersion.Version, "v1alpha1"; got != want {
		t.Errorf("GroupVersion.Version = %q; want %q", got, want)
	}
}

// TestSchemeRegistration confirms both CRD types register cleanly
// against a controller-runtime scheme. The reconciler in Phase 2
// calls AddToScheme at startup; if it errors at runtime the
// controller crash-loops, so cover it in tests.
func TestSchemeRegistration(t *testing.T) {
	s := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(s))

	for _, kind := range []string{"TrafficMonitor", "TrafficMonitorList", "ClusterTrafficPolicy", "ClusterTrafficPolicyList"} {
		gvk := v1alpha1.GroupVersion.WithKind(kind)
		if !s.Recognizes(gvk) {
			t.Errorf("scheme does not recognize %s", gvk)
		}
	}
}

// TestTrafficMonitor_DeepCopy round-trips a TrafficMonitor through
// the controller-gen-generated DeepCopy and verifies structural
// equality. Catches stale codegen if someone edits the types but
// forgets to re-run dev/scripts/codegen.sh.
func TestTrafficMonitor_DeepCopy(t *testing.T) {
	orig := &v1alpha1.TrafficMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: "test-monitor", Namespace: "payments"},
		Spec: v1alpha1.TrafficMonitorSpec{
			WorkloadSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "payments-api"}},
			MonitoringSpecCore: v1alpha1.MonitoringSpecCore{
				Protocols: v1alpha1.ProtocolSet{
					L4:   &v1alpha1.L4Config{Enabled: true},
					HTTP: &v1alpha1.HTTPConfig{Enabled: true, Ports: []int32{8080, 8443}},
				},
			},
		},
		Status: v1alpha1.TrafficMonitorStatus{
			MonitoringStatusCore: v1alpha1.MonitoringStatusCore{
				ObservedGeneration: 7,
				MatchedPodCount:    3,
				MatchedPodSample:   []string{"payments-api-abc", "payments-api-def"},
				Conditions: []metav1.Condition{{
					Type:   v1alpha1.ConditionReady,
					Status: metav1.ConditionTrue,
					Reason: "AllPodsCovered",
				}},
			},
		},
	}
	cp := orig.DeepCopy()
	if !reflect.DeepEqual(orig, cp) {
		t.Errorf("DeepCopy not equal to original\norig: %+v\ncopy: %+v", orig, cp)
	}
	// Mutate the copy; original must not change.
	cp.Spec.MonitoringSpecCore.Protocols.HTTP.Ports[0] = 9999
	if orig.Spec.MonitoringSpecCore.Protocols.HTTP.Ports[0] == 9999 {
		t.Error("DeepCopy returned shared HTTP.Ports slice — generator output is wrong")
	}
}

// TestClusterTrafficPolicy_DeepCopy mirrors TestTrafficMonitor_DeepCopy
// for the cluster-scoped CRD.
func TestClusterTrafficPolicy_DeepCopy(t *testing.T) {
	orig := &v1alpha1.ClusterTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-default"},
		Spec: v1alpha1.ClusterTrafficPolicySpec{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "prod"}},
			MonitoringSpecCore: v1alpha1.MonitoringSpecCore{
				Protocols: v1alpha1.ProtocolSet{
					L4: &v1alpha1.L4Config{Enabled: true},
				},
			},
		},
	}
	cp := orig.DeepCopy()
	if !reflect.DeepEqual(orig, cp) {
		t.Errorf("DeepCopy not equal\norig: %+v\ncopy: %+v", orig, cp)
	}
	cp.Spec.NamespaceSelector.MatchLabels["tier"] = "dev"
	if orig.Spec.NamespaceSelector.MatchLabels["tier"] == "dev" {
		t.Error("DeepCopy returned shared NamespaceSelector.MatchLabels map")
	}
}

// TestTrafficMonitor_JSON pins the on-the-wire JSON shape. Catches
// accidental field-name churn (the API surface is part of the public
// contract; renaming a field is a breaking change).
func TestTrafficMonitor_JSON(t *testing.T) {
	in := []byte(`{
		"apiVersion": "ollie.gke-labs.dev/v1alpha1",
		"kind": "TrafficMonitor",
		"metadata": { "name": "x", "namespace": "y" },
		"spec": {
			"workloadSelector": { "matchLabels": { "app": "nginx" } },
			"protocols": {
				"http": { "enabled": true, "ports": [80, 443] }
			}
		}
	}`)
	var tm v1alpha1.TrafficMonitor
	if err := json.Unmarshal(in, &tm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := tm.Spec.WorkloadSelector.MatchLabels["app"]; got != "nginx" {
		t.Errorf("workloadSelector.matchLabels.app = %q; want nginx", got)
	}
	if tm.Spec.MonitoringSpecCore.Protocols.HTTP == nil {
		t.Fatal("Protocols.HTTP nil; expected populated")
	}
	if got, want := tm.Spec.MonitoringSpecCore.Protocols.HTTP.Enabled, true; got != want {
		t.Errorf("HTTP.Enabled = %v; want %v", got, want)
	}
	if got, want := tm.Spec.MonitoringSpecCore.Protocols.HTTP.Ports, []int32{80, 443}; !reflect.DeepEqual(got, want) {
		t.Errorf("HTTP.Ports = %v; want %v", got, want)
	}
}
