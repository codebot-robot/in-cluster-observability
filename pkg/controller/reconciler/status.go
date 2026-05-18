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

package reconciler

import (
	"context"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/gke-labs/in-cluster-observability/pkg/controller/api/v1alpha1"
)

// AgentReporter is the subset of stream.Dispatcher's agent-state
// surface the status writer needs. Returns the count of pods on each
// node that the corresponding agent has confirmed as actively
// monitored (via AgentStatus). Defined as an interface so the
// reconciler doesn't import the stream package directly (cycle-free).
type AgentReporter interface {
	ActivelyMonitoredCount(podUIDs map[string]bool) int32
}

// WriteStatuses applies the rollups from a CoverageResult onto every
// affected TrafficMonitor / ClusterTrafficPolicy. Idempotent: if the
// computed status equals what's currently on the CR, the patch is a
// no-op. Per-CR errors are accumulated and returned; a single bad
// patch does not abort the rest of the writeback.
func WriteStatuses(
	ctx context.Context,
	c client.Client,
	tms []*v1alpha1.TrafficMonitor,
	ctps []*v1alpha1.ClusterTrafficPolicy,
	cov *CoverageResult,
	agents AgentReporter,
) error {
	var errs []string
	for _, tm := range tms {
		key := types.NamespacedName{Namespace: tm.Namespace, Name: tm.Name}
		desired := tmDesiredStatus(tm, cov.TMStatus[key], agents)
		if statusEqual(tm.Status.MonitoringStatusCore, desired) {
			continue
		}
		patched := tm.DeepCopy()
		patched.Status.MonitoringStatusCore = desired
		if err := c.Status().Patch(ctx, patched, client.MergeFrom(tm)); err != nil {
			errs = append(errs, "tm "+key.String()+": "+err.Error())
		}
	}
	for _, ctp := range ctps {
		desired := ctpDesiredStatus(ctp, cov.CTPStatus[ctp.Name], agents)
		if statusEqual(ctp.Status.MonitoringStatusCore, desired) {
			continue
		}
		patched := ctp.DeepCopy()
		patched.Status.MonitoringStatusCore = desired
		if err := c.Status().Patch(ctx, patched, client.MergeFrom(ctp)); err != nil {
			errs = append(errs, "ctp "+ctp.Name+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return &writeStatusErr{messages: errs}
	}
	return nil
}

type writeStatusErr struct{ messages []string }

func (e *writeStatusErr) Error() string { return "WriteStatuses: " + strings.Join(e.messages, "; ") }

func tmDesiredStatus(tm *v1alpha1.TrafficMonitor, st *CRStatus, agents AgentReporter) v1alpha1.MonitoringStatusCore {
	return makeDesired(tm.Generation, st, agents)
}

func ctpDesiredStatus(ctp *v1alpha1.ClusterTrafficPolicy, st *CRStatus, agents AgentReporter) v1alpha1.MonitoringStatusCore {
	return makeDesired(ctp.Generation, st, agents)
}

func makeDesired(observedGen int64, st *CRStatus, agents AgentReporter) v1alpha1.MonitoringStatusCore {
	if st == nil {
		st = &CRStatus{}
	}
	out := v1alpha1.MonitoringStatusCore{
		ObservedGeneration: observedGen,
		MatchedPodCount:    int32(len(st.MatchedPods)),
	}
	// Sample up to 5 covered pod names, sorted for determinism.
	names := make([]string, 0, len(st.CoveredPods))
	for _, p := range st.CoveredPods {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = names[:5]
	}
	out.MatchedPodSample = names

	// Conflicts list (sorted for determinism).
	conflicts := append([]string(nil), st.Conflicts...)
	sort.Strings(conflicts)
	out.Conflicts = conflicts

	// Actively-monitored count via the agent reporter (counts pods
	// where the agent has reported back AgentStatus.active=true).
	if agents != nil {
		covered := map[string]bool{}
		for _, p := range st.CoveredPods {
			covered[string(p.UID)] = true
		}
		out.ActivelyMonitoredPodCount = agents.ActivelyMonitoredCount(covered)
	}

	// Conditions.
	now := metav1.Now()
	ready := metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "AllPodsCovered",
		Message:            "All matched pods have specs applied",
		LastTransitionTime: now,
	}
	if len(st.MatchedPods) == 0 {
		ready.Status = metav1.ConditionFalse
		ready.Reason = "NoPodsMatched"
		ready.Message = "Selector matched no pods"
	} else if out.ActivelyMonitoredPodCount < int32(len(st.CoveredPods)) {
		ready.Status = metav1.ConditionFalse
		ready.Reason = "PendingAgents"
		ready.Message = "Some covered pods have not yet reported active monitoring"
	}
	out.Conditions = append(out.Conditions, ready)

	if len(conflicts) > 0 {
		out.Conditions = append(out.Conditions, metav1.Condition{
			Type:               v1alpha1.ConditionConflict,
			Status:             metav1.ConditionTrue,
			Reason:             "ConflictsWith",
			Message:            "Overlaps with: " + strings.Join(conflicts, ", "),
			LastTransitionTime: now,
		})
		// Conflict displaces Ready=True (per ADR-0022.4 reactive
		// conflict-detection model).
		for i := range out.Conditions {
			if out.Conditions[i].Type == v1alpha1.ConditionReady {
				out.Conditions[i].Status = metav1.ConditionFalse
				out.Conditions[i].Reason = "ConflictsWith"
				out.Conditions[i].Message = ready.Message + "; but " + strings.Join(conflicts, ", ")
			}
		}
	}
	return out
}

// statusEqual is the no-op short-circuit. Compares the fields the
// reconciler computes; ignores LastTransitionTime so an unchanged
// status doesn't generate a patch every reconcile tick.
func statusEqual(a, b v1alpha1.MonitoringStatusCore) bool {
	if a.ObservedGeneration != b.ObservedGeneration {
		return false
	}
	if a.MatchedPodCount != b.MatchedPodCount {
		return false
	}
	if a.ActivelyMonitoredPodCount != b.ActivelyMonitoredPodCount {
		return false
	}
	if !stringSliceEq(a.MatchedPodSample, b.MatchedPodSample) {
		return false
	}
	if !stringSliceEq(a.Conflicts, b.Conflicts) {
		return false
	}
	if !conditionsEq(a.Conditions, b.Conditions) {
		return false
	}
	return true
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

func conditionsEq(a, b []metav1.Condition) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type ||
			a[i].Status != b[i].Status ||
			a[i].Reason != b[i].Reason ||
			a[i].Message != b[i].Message {
			return false
		}
	}
	return true
}

// touchLastTransition is unused but kept for the (planned) Phase 5
// optimization where we only bump LastTransitionTime when the
// condition actually transitions. Today every status write resets
// it; that's noisy but correct.
var _ = touchLastTransition

func touchLastTransition() time.Time { return time.Now() }
