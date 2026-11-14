package agent

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// fakeWorkloads serves one rolled-out Deployment per namespace it knows.
type fakeWorkloads map[string][]*unstructured.Unstructured

func (f fakeWorkloads) Workloads(namespace string, _ map[string]string) ([]*unstructured.Unstructured, error) {
	objects, ok := f[namespace]
	if !ok {
		return nil, targets.ErrNotWatched
	}
	return objects, nil
}

func rolledOut(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": name, "generation": int64(1)},
		"spec":     map[string]any{"replicas": int64(1)},
		"status": map[string]any{
			"observedGeneration": int64(1), "replicas": int64(1), "updatedReplicas": int64(1),
			"readyReplicas": int64(1), "availableReplicas": int64(1),
			"conditions": []any{map[string]any{"type": "Available", "status": "True"}},
		},
	}}
}

func TestRollouts_ReportsAProgressingDeploymentHealthy(t *testing.T) {
	current := "dep_0"
	set := targets.Set{Targets: []targets.Target{{TargetView: apiclient.TargetView{
		Id: "tgt_1", Namespace: "payments",
		Applications: []apiclient.ApplicationView{
			{Id: "app_1", WorkloadSelector: map[string]string{"app": "payments"}},
			{Id: "app_2", CurrentDeploymentId: &current},
		},
	}}}}
	assignments := []apiclient.DeploymentAssignment{
		{Deployment: apiclient.DeploymentSnapshot{Id: "dep_1", ApplicationId: "app_1", Status: apiclient.DeploymentSnapshotStatusProgressing},
			Target: apiclient.TargetSnapshot{Id: "tgt_1"}, Apply: apiclient.ApplyAssignment{Namespace: "payments"}},
		{Deployment: apiclient.DeploymentSnapshot{Id: "dep_9", Status: apiclient.DeploymentSnapshotStatusApplying}},
	}

	items := toWatch(assignments, set)
	require.Len(t, items, 2, "the progressing assignment and the other placement's current Deployment")
	assert.Equal(t, map[string]string{"app": "payments"}, items[0].selector)
	assert.Equal(t, map[string]string{"ivaldi.build/application": "app_2"}, items[1].selector)

	cp := &fakeControlPlane{}
	log := slog.New(slog.DiscardHandler)
	r := newRollouts(fakeWorkloads{"payments": {rolledOut("api")}}, report.New(cp, log), log)
	r.check(t.Context(), items)
	// The current Deployment is already healthy, so only dep_1 moves.
	assert.Equal(t, []string{"dep_1:healthy"}, cp.reportIDs())
	assert.Equal(t, apiclient.ReportDeploymentRequestBodyStatusHealthy, *cp.reports[0].Status)

	// Once reported, nothing is reported again.
	r.check(t.Context(), items)
	assert.Len(t, cp.reportIDs(), 1)
}
