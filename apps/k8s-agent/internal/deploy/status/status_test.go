package status

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// deployment is a Deployment whose rollout is done when updated equals
// replicas and the controller has seen the latest generation.
func deployment(name string, replicas, updated int64, deadlineExceeded bool) *unstructured.Unstructured {
	conditions := []any{map[string]any{"type": "Available", "status": "True"}}
	if deadlineExceeded {
		conditions = append(conditions, map[string]any{"type": "Progressing", "status": "False", "reason": "ProgressDeadlineExceeded"})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": name, "generation": int64(2)},
		"spec":     map[string]any{"replicas": replicas},
		"status": map[string]any{
			"observedGeneration": int64(2), "replicas": replicas, "updatedReplicas": updated,
			"readyReplicas": updated, "availableReplicas": updated, "conditions": conditions,
		},
	}}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name    string
		objects []*unstructured.Unstructured
		want    Overall
	}{
		{name: "no workloads", want: Current},
		{name: "all rolled out", objects: []*unstructured.Unstructured{deployment("api", 2, 2, false)}, want: Current},
		{name: "one still rolling out", objects: []*unstructured.Unstructured{deployment("api", 2, 2, false), deployment("worker", 2, 1, false)}, want: InProgress},
		{name: "one past its progress deadline", objects: []*unstructured.Unstructured{deployment("worker", 2, 1, false), deployment("api", 2, 1, true)}, want: Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, workloads := Evaluate(tt.objects)
			assert.Equal(t, tt.want, got)
			assert.Len(t, workloads, len(tt.objects))
		})
	}
}

func TestTracker_Next(t *testing.T) {
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	type step struct {
		after    time.Duration
		known    release.DeploymentStatus
		overall  Overall
		count    int
		want     release.DeploymentStatus
		reported bool // the move was reported
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{name: "a rollout becomes healthy", steps: []step{
			{known: release.DeploymentProgressing, overall: InProgress, count: 1},
			{after: time.Second, known: release.DeploymentProgressing, overall: Current, count: 1, want: release.DeploymentHealthy},
		}},
		{name: "a rollout fails", steps: []step{
			{known: release.DeploymentProgressing, overall: Failed, count: 1, want: release.DeploymentFailed},
		}},
		{name: "no workloads wait for the grace period", steps: []step{
			{known: release.DeploymentProgressing, overall: Current},
			{after: 31 * time.Second, known: release.DeploymentProgressing, overall: Current, want: release.DeploymentHealthy},
		}},
		{name: "degraded is debounced, then healthy again", steps: []step{
			{overall: InProgress, count: 1},
			{after: 59 * time.Second, overall: InProgress, count: 1},
			{after: 60 * time.Second, overall: InProgress, count: 1, want: release.DeploymentDegraded, reported: true},
			{after: 61 * time.Second, overall: Current, count: 1, want: release.DeploymentHealthy},
		}},
		{name: "a blip shorter than the debounce is not degraded", steps: []step{
			{overall: InProgress, count: 1},
			{after: 30 * time.Second, overall: Current, count: 1},
			{after: 70 * time.Second, overall: InProgress, count: 1},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewTracker(60*time.Second, 30*time.Second)
			for i, s := range tt.steps {
				got := tracker.Next("dep_1", s.known, s.overall, s.count, start.Add(s.after))
				assert.Equal(t, s.want, got, "step %d", i)
				if s.reported {
					tracker.Moved("dep_1", got)
				}
			}
		})
	}
}
