package helm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/values"
)

const deploymentTemplate = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-api
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels: {app: payments-api}
  template:
    metadata:
      labels: {app: payments-api}
    spec:
      containers:
        - name: api
          image: {{ .Values.global.ivaldi.artifacts.api.image }}
          env:
            - name: ENVIRONMENT
              value: {{ .Values.environment | quote }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: payments-config
data:
  replicas: "{{ .Values.replicas }}"
`

func testChart() *chart.Chart {
	return &chart.Chart{
		Metadata:  &chart.Metadata{APIVersion: "v2", Name: "payments", Version: "1.0.0"},
		Templates: []*common.File{{Name: "templates/deployment.yaml", Data: []byte(deploymentTemplate)}},
		Values:    map[string]any{"replicas": 1, "environment": "unset"},
		Raw: []*common.File{
			{Name: "values.production.yaml", Data: []byte("replicas: 3\nenvironment: \"#{Ivaldi.Environment.Slug}\"\n")},
		},
	}
}

func assignment(deploymentID string) deploy.Assignment {
	return deploy.Assignment{
		DeploymentID: deploymentID, ApplicationID: "app_1", ReleaseID: "rel_1", Version: "1.4.0",
		TargetID: "tgt_1", Namespace: "payments-prod", Deployer: Name,
		Definition:      deploy.Artifact{Repository: "ghcr.io/acme/charts/payments", Digest: "sha256:chart"},
		Overlays:        []string{"values.production.yaml"},
		Images:          map[string]values.Image{"api": {Repository: "ghcr.io/acme/api", Digest: "sha256:abc"}},
		System:          map[string]string{"Ivaldi.Environment.Slug": "production"},
		HelmReleaseName: "payments",
	}
}

type fixture struct {
	deployer *deployer
	store    *storage.Storage
	loads    int
	steps    []deploy.Step
	// kube, when set, is the Kubernetes client Helm writes through.
	kube kube.Interface
}

func (f *fixture) kubeClient() kube.Interface {
	if f.kube != nil {
		return f.kube
	}
	return &kubefake.PrintingKubeClient{Out: io.Discard}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: storage.Init(driver.NewMemory())}
	f.deployer = &deployer{
		cfg: Config{Timeout: 10 * time.Minute, RESTConfig: &rest.Config{}},
		now: time.Now,
		configure: func(string, *rest.Config) (*action.Configuration, error) {
			return &action.Configuration{
				Releases:     f.store,
				KubeClient:   f.kubeClient(),
				Capabilities: common.DefaultCapabilities,
			}, nil
		},
		load: func(context.Context, deploy.Artifact) (*chart.Chart, error) {
			f.loads++
			return testChart(), nil
		},
	}
	return f
}

func (f *fixture) progress(step deploy.Step, _ string) { f.steps = append(f.steps, step) }

func TestApply_InstallsUpgradesAndSkipsAnAppliedDeployment(t *testing.T) {
	f := newFixture(t)

	applied, err := f.deployer.Apply(t.Context(), assignment("dep_1"), f.progress)
	require.NoError(t, err)
	assert.False(t, applied.Skipped)
	assert.Equal(t, map[string]any{"type": "helm", "name": "payments", "revision": "1"}, applied.Ref)
	assert.Equal(t, []deploy.Step{deploy.StepFetching, deploy.StepResolving, deploy.StepApplying}, f.steps)

	rel, err := latest(f.store, "payments")
	require.NoError(t, err)
	assert.Equal(t, releasecommon.StatusDeployed, rel.Info.Status)
	assert.Equal(t, "dep_1", rel.Labels[releaseLabel])
	assert.Equal(t, "ssa", rel.ApplyMethod)
	// kyaml quotes some values it sets; the test does not care how.
	manifest := strings.NewReplacer("'", "", `"`, "").Replace(rel.Manifest)
	for _, want := range []string{
		"replicas: 3",
		"image: ghcr.io/acme/api@sha256:abc",
		"value: production",
		"ivaldi.build/deployment: dep_1",
		"ivaldi.build/release: rel_1",
		"ivaldi.build/application: app_1",
	} {
		assert.Contains(t, manifest, want)
	}
	// The application label is on the Deployment and its Pod template, not
	// the ConfigMap.
	assert.Equal(t, 2, strings.Count(manifest, "ivaldi.build/application: app_1"))

	// A restart that finds the Deployment applied does not apply it again.
	f.steps = nil
	applied, err = f.deployer.Apply(t.Context(), assignment("dep_1"), f.progress)
	require.NoError(t, err)
	assert.True(t, applied.Skipped)
	assert.Equal(t, 1, f.loads)
	assert.Empty(t, f.steps)

	applied, err = f.deployer.Apply(t.Context(), assignment("dep_2"), f.progress)
	require.NoError(t, err)
	assert.Equal(t, "2", applied.Ref["revision"])
}

func TestApply_FailsOnAMissingOverlay(t *testing.T) {
	f := newFixture(t)
	a := assignment("dep_1")
	a.Overlays = []string{"values.staging.yaml"}

	_, err := f.deployer.Apply(t.Context(), a, f.progress)
	var failure *deploy.Failure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "overlay values.staging.yaml is not in the chart", failure.Reason)
	_, err = latest(f.store, "payments")
	assert.ErrorIs(t, err, driver.ErrReleaseNotFound, "nothing was applied")
}

func TestPrepare_PendingRevisions(t *testing.T) {
	timeout := 10 * time.Minute
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		deployment  string // the label on the pending revision; "" for none
		age         time.Duration
		history     bool // an earlier revision was deployed
		wantInstall *bool
		wantFailure string
		wantStep    bool
	}{
		{name: "ours, interrupted, after a deployed revision", deployment: "dep_1", history: true, wantInstall: new(false), wantStep: true},
		{name: "ours, interrupted first install", deployment: "dep_1", wantInstall: new(true), wantStep: true},
		{name: "another Deployment's, still fresh", deployment: "dep_0", age: time.Minute, history: true, wantFailure: "has another operation in progress"},
		{name: "unlabelled, still fresh", age: time.Minute, history: true, wantFailure: "has another operation in progress"},
		{name: "another Deployment's, abandoned", deployment: "dep_0", age: time.Hour, history: true, wantInstall: new(false), wantStep: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.Init(driver.NewMemory())
			version := 1
			if tt.history {
				require.NoError(t, store.Create(&releasev1.Release{
					Name: "payments", Namespace: "payments-prod", Version: 1,
					Info: &releasev1.Info{Status: releasecommon.StatusDeployed, LastDeployed: now.Add(-24 * time.Hour)},
				}))
				version = 2
			}
			pending := releasecommon.StatusPendingUpgrade
			if !tt.history {
				pending = releasecommon.StatusPendingInstall
			}
			labels := map[string]string{}
			if tt.deployment != "" {
				labels[releaseLabel] = tt.deployment
			}
			require.NoError(t, store.Create(&releasev1.Release{
				Name: "payments", Namespace: "payments-prod", Version: version, Labels: labels,
				Info: &releasev1.Info{Status: pending, LastDeployed: now.Add(-tt.age)},
			}))

			d := &deployer{cfg: Config{Timeout: timeout}, now: func() time.Time { return now }}
			var steps []deploy.Step
			install, err := d.prepare(store, assignment("dep_1"), func(s deploy.Step, _ string) { steps = append(steps, s) })
			if tt.wantFailure != "" {
				var failure *deploy.Failure
				require.ErrorAs(t, err, &failure)
				assert.Contains(t, failure.Reason, tt.wantFailure)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantInstall, install)
			assert.Equal(t, tt.wantStep, len(steps) == 1 && steps[0] == deploy.StepInterrupted)
			if *install {
				_, err := latest(store, "payments")
				assert.ErrorIs(t, err, driver.ErrReleaseNotFound, "the failed first install is removed")
				return
			}
			rel, err := latest(store, "payments")
			require.NoError(t, err)
			assert.Equal(t, releasecommon.StatusFailed, rel.Info.Status, "the pending revision is marked failed")
		})
	}
}

func TestPostRenderer_RefusesClusterScopedObjects(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, meta.RESTScopeRoot)
	p := &postRenderer{applicationID: "app_1", deploymentID: "dep_1", releaseID: "rel_1", mapper: mapper}

	_, err := p.Run(bytes.NewBufferString("apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: admin-ish\n"))
	require.ErrorContains(t, err, "the chart creates ClusterRole admin-ish, which is cluster-scoped")
}

// blockingKube is a Kubernetes client that blocks inside Helm's install
// goroutine, as a write to a slow API server does: every install asks it for
// a waiter after applying, in the goroutine RunWithContext abandons on
// cancellation.
type blockingKube struct {
	kubefake.PrintingKubeClient
	called  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingKube) block() {
	b.once.Do(func() { close(b.called) })
	<-b.release
}

func (b *blockingKube) GetWaiter(strategy kube.WaitStrategy) (kube.Waiter, error) {
	b.block()
	return b.PrintingKubeClient.GetWaiter(strategy)
}

func (b *blockingKube) GetWaiterWithOptions(strategy kube.WaitStrategy, opts ...kube.WaitOption) (kube.Waiter, error) {
	b.block()
	return b.PrintingKubeClient.GetWaiterWithOptions(strategy, opts...)
}

func TestApply_ReturnsOnlyOnceHelmHasStoppedWriting(t *testing.T) {
	f := newFixture(t)
	blocking := &blockingKube{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}, called: make(chan struct{}), release: make(chan struct{})}
	f.kube = blocking
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() {
		_, err := f.deployer.Apply(ctx, assignment("dep_1"), func(deploy.Step, string) {})
		done <- err
	}()

	select {
	case <-blocking.called:
	case err := <-done:
		t.Fatalf("Apply returned before Helm wrote anything: %v", err)
	}
	cancel()
	select {
	case <-done:
		t.Fatal("Apply returned while Helm was still writing")
	case <-time.After(200 * time.Millisecond):
	}
	// The fake API server ignores cancellation, so the released install
	// finishes; against a real one the bound requests fail instead. Either
	// way Apply returns only after Helm's goroutine has.
	close(blocking.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Apply did not return once Helm stopped")
	}
}

func TestBoundTransport_StopsRequestsWhenTheApplyEnds(t *testing.T) {
	stalled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fast" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		close(stalled)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	client := &http.Client{Transport: bind(ctx, nil)}

	resp, err := client.Get(server.URL + "/fast")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "ok", string(body), "a response is readable after RoundTrip returns")

	done := make(chan error)
	go func() {
		_, err := client.Get(server.URL + "/stall")
		done <- err
	}()
	<-stalled
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("a request in flight was not cancelled")
	}
	_, err = client.Get(server.URL + "/fast")
	require.ErrorIs(t, err, context.Canceled, "no new request after the apply ends")
}

func TestPostRenderer_RefusesObjectsInAnotherNamespace(t *testing.T) {
	p := &postRenderer{applicationID: "app_1", deploymentID: "dep_1", releaseID: "rel_1", namespace: "payments-staging"}

	_, err := p.Run(bytes.NewBufferString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\n  namespace: payments-prod\n"))
	require.ErrorContains(t, err, "the chart puts ConfigMap cfg in namespace payments-prod; this Deployment may only change payments-staging")

	_, err = p.Run(bytes.NewBufferString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\n  namespace: payments-staging\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unset\n"))
	require.NoError(t, err, "its own namespace, or none, is fine")
}

func TestApply_RefusesAHookInAnotherNamespace(t *testing.T) {
	f := newFixture(t)
	f.deployer.load = func(context.Context, deploy.Artifact) (*chart.Chart, error) {
		c := testChart()
		c.Templates = append(c.Templates, &common.File{Name: "templates/hook.yaml", Data: []byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: migrate\n  namespace: payments-staging\n  annotations:\n    helm.sh/hook: pre-install\n")})
		return c, nil
	}
	_, err := f.deployer.Apply(t.Context(), assignment("dep_1"), f.progress)
	require.ErrorContains(t, err, "in namespace payments-staging; this Deployment may only change payments-prod")
}

func TestStuckRevision_TheRecoveryCommandClearsIt(t *testing.T) {
	kube := k8sfake.NewClientset()
	store := storage.Init(driver.NewSecrets(kube.CoreV1().Secrets("payments-prod")))
	require.NoError(t, store.Create(&releasev1.Release{Name: "payments", Namespace: "payments-prod", Version: 1,
		Info: &releasev1.Info{Status: releasecommon.StatusDeployed}}))
	require.NoError(t, store.Create(&releasev1.Release{Name: "payments", Namespace: "payments-prod", Version: 2,
		Info: &releasev1.Info{Status: releasecommon.StatusPendingUpgrade}}))

	failure := stuck(assignment("dep_1"), &releasev1.Release{Version: 2, Info: &releasev1.Info{Status: releasecommon.StatusPendingUpgrade}}, errors.New("update refused"))
	command := failure.Detail["clear_with"].(string)
	assert.Equal(t, "kubectl -n payments-prod delete secret sh.helm.release.v1.payments.v2", command)

	// Run what the command does, against Helm's own Secret storage.
	name := strings.Fields(command)[len(strings.Fields(command))-1]
	require.NoError(t, kube.CoreV1().Secrets("payments-prod").Delete(t.Context(), name, metav1.DeleteOptions{}))
	rel, err := latest(store, "payments")
	require.NoError(t, err)
	assert.Equal(t, 1, rel.Version)
	assert.Equal(t, releasecommon.StatusDeployed, rel.Info.Status, "the pending revision is gone, so the next apply upgrades")
}
