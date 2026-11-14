package agent

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/release"
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

// annotated is a rolled-out Deployment that Ivaldi Deployment dep applied.
func annotated(name, dep string) *unstructured.Unstructured {
	obj := rolledOut(name)
	obj.SetAnnotations(map[string]string{"ivaldi.build/deployment": dep})
	return obj
}

func progressing(dep string) watched {
	return watched{deploymentID: dep, namespace: "payments", known: release.DeploymentProgressing}
}

func TestRollouts_WaitForTheCachesToShowTheDeployment(t *testing.T) {
	cp := &fakeControlPlane{}
	log := slog.New(slog.DiscardHandler)
	workloads := fakeWorkloads{"payments": {annotated("api", "dep_old")}}
	r := newRollouts(workloads, report.New(cp, log), log)

	r.check(t.Context(), []watched{progressing("dep_new")})
	assert.Empty(t, cp.reportIDs(), "the old revision's workloads say nothing about dep_new")

	workloads["payments"] = []*unstructured.Unstructured{annotated("api", "dep_new"), rolledOut("sidecar-operator")}
	r.check(t.Context(), []watched{progressing("dep_new")})
	assert.Equal(t, []string{"dep_new:healthy"}, cp.reportIDs())
}

func TestRollouts_AResumedAgentReportsRecoveryFromDegraded(t *testing.T) {
	current := "dep_1"
	degraded := apiclient.ApplicationViewCurrentDeploymentStatus("degraded")
	set := targets.Set{Targets: []targets.Target{{TargetView: apiclient.TargetView{
		Id: "tgt_1", Namespace: "payments",
		Applications: []apiclient.ApplicationView{{Id: "app_1", CurrentDeploymentId: &current, CurrentDeploymentStatus: &degraded}},
	}}}}
	cp := &fakeControlPlane{}
	log := slog.New(slog.DiscardHandler)
	r := newRollouts(fakeWorkloads{"payments": {rolledOut("api")}}, report.New(cp, log), log)

	r.check(t.Context(), toWatch(nil, set))
	require.Len(t, cp.reports, 1)
	assert.Equal(t, apiclient.ReportDeploymentRequestBodyStatusHealthy, *cp.reports[0].Status)
}

func TestRollouts_AFailingReportDoesNotHoldUpTheLoop(t *testing.T) {
	cp := &fakeControlPlane{reportErr: &apiclient.StatusError{Operation: "report", Status: http.StatusServiceUnavailable}}
	log := slog.New(slog.DiscardHandler)
	r := newRollouts(fakeWorkloads{"payments": {annotated("api", "dep_1")}}, report.New(cp, log), log)
	r.reportTimeout = 50 * time.Millisecond

	start := time.Now()
	r.check(t.Context(), []watched{progressing("dep_1")})
	assert.Less(t, time.Since(start), 2*time.Second, "the check gives up on the report")

	// The move was not recorded, so the next check tries again.
	cp.mu.Lock()
	cp.reportErr = nil
	cp.mu.Unlock()
	r.check(t.Context(), []watched{progressing("dep_1")})
	assert.Contains(t, cp.reportIDs(), "dep_1:healthy")
}
