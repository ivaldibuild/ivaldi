package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/auth"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

func TestConfig(t *testing.T) {
	noFile := func(string) ([]byte, error) { return nil, errors.New("no file") }
	podNamespace := func(string) ([]byte, error) { return []byte("ivaldi-system\n"), nil }
	hostname := func() (string, error) { return "ivaldi-agent-7d9f", nil }

	tests := []struct {
		name     string
		args     []string
		env      map[string]string
		readFile func(string) ([]byte, error)
		wantErr  string
		check    func(*testing.T, Config)
	}{
		{
			name:     "defaults come from the Pod",
			args:     []string{"--server-url", "https://ivaldi.example.com"},
			readFile: podNamespace,
			check: func(t *testing.T, c Config) {
				assert.Equal(t, "ivaldi-system", c.Namespace)
				assert.Equal(t, "ivaldi-agent-7d9f", c.PodName)
				assert.Equal(t, "kubernetes", c.AuthenticationMethod)
			},
		},
		{
			name:     "environment variables set flags the command line does not",
			args:     []string{"--authentication-method", "bearer"},
			env:      map[string]string{"IVALDI_SERVER_URL": "https://ivaldi.example.com", "IVALDI_AUTHENTICATION_METHOD": "mtls", "IVALDI_NAMESPACE": "agents"},
			readFile: noFile,
			check: func(t *testing.T, c Config) {
				assert.Equal(t, "https://ivaldi.example.com", c.ServerURL)
				assert.Equal(t, "bearer", c.AuthenticationMethod)
				assert.Equal(t, "agents", c.Namespace)
			},
		},
		{name: "server URL is required", readFile: podNamespace, wantErr: "--server-url is required"},
		{name: "server URL must be a URL", args: []string{"--server-url", "ftp://x"}, readFile: podNamespace, wantErr: "must be an https URL"},
		{name: "server URL must use https", args: []string{"--server-url", "http://ivaldi.example.com"}, readFile: podNamespace, wantErr: "must use https"},
		{
			name:     "http is allowed when asked for",
			args:     []string{"--server-url", "http://localhost:8660", "--allow-insecure-http"},
			readFile: podNamespace,
			check:    func(t *testing.T, c Config) { assert.True(t, c.AllowInsecureHTTP) },
		},
		{name: "unknown method", args: []string{"--server-url", "https://x", "--authentication-method", "magic"}, readFile: podNamespace, wantErr: "must be kubernetes, bearer, or mtls"},
		{name: "mtls needs a certificate", args: []string{"--server-url", "https://x", "--authentication-method", "mtls"}, readFile: podNamespace, wantErr: "--client-cert-file and --client-key-file are required"},
		{name: "namespace is required outside a Pod", args: []string{"--server-url", "https://x"}, readFile: noFile, wantErr: "--namespace is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			flags := pflag.NewFlagSet("agent", pflag.ContinueOnError)
			cfg.bind(flags)
			require.NoError(t, flags.Parse(tt.args))
			require.NoError(t, bindEnv(flags, func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok }))

			err := cfg.complete(tt.readFile, hostname)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestReadiness(t *testing.T) {
	r := newReadiness(conditionEnrolled, conditionLease)
	server := httptest.NewServer(r.handler())
	defer server.Close()
	get := func(path string) (int, string) {
		resp, err := http.Get(server.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	code, _ := get("/healthz")
	assert.Equal(t, http.StatusOK, code)
	code, body := get("/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, body, "enrolled, lease")

	r.set(conditionEnrolled, true)
	r.set(conditionLease, true)
	code, _ = get("/readyz")
	assert.Equal(t, http.StatusOK, code)
}

// fakeCredentials enrols as told and loses the credential when told.
type fakeCredentials struct {
	mu       sync.Mutex
	enrolErr error
	enrols   int
	lose     chan struct{}
	// onEnrol, when set, runs at each Enrol.
	onEnrol func()
}

func newFakeCredentials() *fakeCredentials { return &fakeCredentials{lose: make(chan struct{})} }

func (f *fakeCredentials) Enrol(context.Context) (state.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onEnrol != nil {
		f.onEnrol()
	}
	f.enrols++
	if f.enrolErr != nil {
		return state.State{}, f.enrolErr
	}
	return state.State{ClusterID: "cls_1", CredentialID: "wcr_1"}, nil
}

func (f *fakeCredentials) Client() *http.Client { return http.DefaultClient }
func (f *fakeCredentials) Invalidate(string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.lose:
	default:
		close(f.lose)
	}
}

func (f *fakeCredentials) Maintain(ctx context.Context) error {
	f.mu.Lock()
	lose := f.lose
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil
	case <-lose:
		return auth.ErrCredentialLost
	}
}

// loseCredential makes the running Maintain lose the credential, and Enrol
// return enrolErr until restore is called.
func (f *fakeCredentials) loseCredential(enrolErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enrolErr = enrolErr
	close(f.lose)
	f.lose = make(chan struct{})
}

func (f *fakeCredentials) restore() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enrolErr = nil
}

func (f *fakeCredentials) enrolCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enrols
}

// fakeControlPlane serves one Target and the assignments it is given, and
// records heartbeats and reports.
type fakeControlPlane struct {
	mu           sync.Mutex
	heartbeats   []apiclient.HeartbeatRequestBody
	heartbeatErr error
	targetsErr   error
	reportErr    error
	assignments  []apiclient.DeploymentAssignment
	reports      []apiclient.ReportDeploymentRequestBody
	observations []apiclient.ObservationRequestBody
	// observedStatus is the status of the Deployment an observation is
	// answered with; empty means one the observation recorded.
	observedStatus apiclient.DeploymentSnapshotStatus
}

func (f *fakeControlPlane) Assignments(context.Context) ([]apiclient.DeploymentAssignment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.assignments), nil
}

func (f *fakeControlPlane) ReportDeployment(_ context.Context, _ string, body apiclient.ReportDeploymentRequestBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, body)
	return f.reportErr
}

func (f *fakeControlPlane) Observe(_ context.Context, body apiclient.ObservationRequestBody) (*apiclient.DeploymentSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observations = append(f.observations, body)
	return &apiclient.DeploymentSnapshot{Status: f.observedStatus}, nil
}

func (f *fakeControlPlane) reportIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.reports {
		out = append(out, r.ReportId)
	}
	return out
}

func (f *fakeControlPlane) Targets(context.Context) (*apiclient.TargetsResponse, error) {
	f.mu.Lock()
	err := f.targetsErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &apiclient.TargetsResponse{
		Cluster:                  apiclient.ClusterView{Id: "cls_1", Slug: "prod-eu"},
		HeartbeatIntervalSeconds: 1, PollIntervalSeconds: 1, WatchIntervalSeconds: 1,
		Targets: []apiclient.TargetView{{Id: "tgt_1", Namespace: "payments"}},
	}, nil
}

func (f *fakeControlPlane) Heartbeat(_ context.Context, body apiclient.HeartbeatRequestBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats = append(f.heartbeats, body)
	return f.heartbeatErr
}

func (f *fakeControlPlane) lastHeartbeat() (apiclient.HeartbeatRequestBody, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.heartbeats) == 0 {
		return apiclient.HeartbeatRequestBody{}, false
	}
	return f.heartbeats[len(f.heartbeats)-1], true
}

func (f *fakeControlPlane) refuseHeartbeats(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeatErr = err
}

func newTestAgent(creds auth.Credentials) (*agent, *fake.Clientset, *fakeControlPlane) {
	kube := fake.NewClientset()
	// The agent may read and change everything in every namespace.
	kube.PrependReactor("create", "selfsubjectrulesreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectRulesReview)
		review.Status.ResourceRules = []authorizationv1.ResourceRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
		return true, review, nil
	})
	kube.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.31.4"}
	cp := &fakeControlPlane{}
	log := slog.New(slog.DiscardHandler)
	return &agent{
		cfg:        Config{Namespace: "ivaldi-system", LeaseName: "ivaldi-agent", PodName: "pod-1", EnrolmentSecret: "enrolment", MaxConcurrentApplies: 2},
		version:    "1.0.0-test",
		log:        log,
		kube:       kube,
		creds:      creds,
		cp:         cp,
		newTracker: func() targets.Tracker { return targets.NewTracker(cp, kube, log) },
		deployers:  map[string]deploy.Deployer{},
		reporter:   report.New(cp, log),
		ready:      newReadiness(conditionEnrolled, conditionLease, conditionTargets),
		enrolRetry: 10 * time.Millisecond,
		retryBase:  10 * time.Millisecond,
	}, kube, cp
}

func TestRun_TakesTheLeaseAndIsReady(t *testing.T) {
	creds := newFakeCredentials()
	a, kube, _ := newTestAgent(creds)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- a.run(ctx) }()

	require.Eventually(t, func() bool { return len(a.ready.unmet()) == 0 }, 5*time.Second, 10*time.Millisecond)
	lease, err := kube.CoordinationV1().Leases("ivaldi-system").Get(t.Context(), "ivaldi-agent", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "pod-1", *lease.Spec.HolderIdentity)

	cancel()
	require.NoError(t, <-done)
}

func TestRun_WaitsNotReadyForANewTokenWhenTheCredentialIsLost(t *testing.T) {
	creds := newFakeCredentials()
	a, kube, _ := newTestAgent(creds)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- a.run(ctx) }()
	require.Eventually(t, func() bool { return len(a.ready.unmet()) == 0 }, 5*time.Second, 10*time.Millisecond)

	creds.loseCredential(auth.ErrNotEnrolled)

	require.Eventually(t, func() bool {
		lease, err := kube.CoordinationV1().Leases("ivaldi-system").Get(t.Context(), "ivaldi-agent", metav1.GetOptions{})
		released := err == nil && (lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "")
		return released && slices.Contains(a.ready.unmet(), conditionEnrolled)
	}, 5*time.Second, 10*time.Millisecond, "the Lease is released and the agent is not ready")

	// It keeps looking for a token at its pace, without exiting.
	enrols := creds.enrolCount()
	require.Eventually(t, func() bool { return creds.enrolCount() > enrols+1 }, 5*time.Second, 10*time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("run exited: %v", err)
	default:
	}

	// A new token gets it working again.
	creds.restore()
	require.Eventually(t, func() bool { return len(a.ready.unmet()) == 0 }, 5*time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

func TestRun_EnrolsOnlyWhileHoldingTheLease(t *testing.T) {
	creds := newFakeCredentials()
	a, kube, _ := newTestAgent(creds)
	var heldAtEnrol []bool
	creds.onEnrol = func() {
		lease, err := kube.CoordinationV1().Leases("ivaldi-system").Get(context.Background(), "ivaldi-agent", metav1.GetOptions{})
		heldAtEnrol = append(heldAtEnrol, err == nil && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == "pod-1")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- a.run(ctx) }()
	require.Eventually(t, func() bool { return len(a.ready.unmet()) == 0 }, 5*time.Second, 10*time.Millisecond)

	// Losing the credential re-enrols, which reloads the state, on the
	// next acquisition.
	creds.loseCredential(auth.ErrNotEnrolled)
	require.Eventually(t, func() bool { return creds.enrolCount() >= 3 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)

	creds.mu.Lock()
	defer creds.mu.Unlock()
	assert.NotContains(t, heldAtEnrol, false, "every Enrol ran while this replica held the Lease")
}

func TestWork_HeartbeatsWhatTheAgentCanDoAndStopsWhenAnotherAgentServesTheCluster(t *testing.T) {
	creds := newFakeCredentials()
	a, _, cp := newTestAgent(creds)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error)
	go func() { done <- a.work(ctx, state.State{ClusterID: "cls_1"}) }()

	// The first heartbeat may come before the Targets are known; the
	// first refresh sends another at once.
	require.Eventually(t, func() bool {
		body, ok := cp.lastHeartbeat()
		return ok && body.Targets != nil && len(*body.Targets) > 0
	}, 5*time.Second, 10*time.Millisecond)
	body, _ := cp.lastHeartbeat()
	assert.Equal(t, "1.0.0-test", body.AgentVersion)
	assert.Equal(t, "v1.31.4", body.KubernetesVersion)
	assert.Equal(t, []apiclient.TargetHeartbeatRequest{{TargetId: "tgt_1", Observe: true, Execute: true}}, *body.Targets)
	require.Eventually(t, func() bool { return !slices.Contains(a.ready.unmet(), conditionTargets) }, 5*time.Second, 10*time.Millisecond)

	cp.refuseHeartbeats(&apiclient.StatusError{Operation: "agent heartbeat", Status: http.StatusForbidden})
	select {
	case err := <-done:
		require.ErrorIs(t, err, auth.ErrCredentialLost)
	case <-time.After(5 * time.Second):
		t.Fatal("work did not stop when the control plane refused the agent")
	}
	assert.Contains(t, a.ready.unmet(), conditionTargets)
}

func TestWork_HeartbeatsWhileTheFirstRefreshFails(t *testing.T) {
	a, _, cp := newTestAgent(newFakeCredentials())
	cp.targetsErr = &apiclient.StatusError{Operation: "agent targets", Status: http.StatusServiceUnavailable}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = a.work(ctx, state.State{ClusterID: "cls_1"}) }()

	require.Eventually(t, func() bool { _, ok := cp.lastHeartbeat(); return ok }, 5*time.Second, 10*time.Millisecond)
	body, _ := cp.lastHeartbeat()
	assert.Equal(t, "v1.31.4", body.KubernetesVersion)
	assert.Equal(t, apiclient.HeartbeatRequestBodyWatchModeNamespaces, body.WatchMode, "a valid watch mode before RBAC is checked")
	assert.Empty(t, *body.Targets, "no Targets before they are known")
	assert.Contains(t, a.ready.unmet(), conditionTargets)
}

func TestWork_SkipsAHeartbeatItCannotFillIn(t *testing.T) {
	a, kube, cp := newTestAgent(newFakeCredentials())
	kube.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("discovery is down")
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = a.work(ctx, state.State{ClusterID: "cls_1"}) }()

	// The refresh still completes; only the heartbeat waits for a version.
	require.Eventually(t, func() bool { return !slices.Contains(a.ready.unmet(), conditionTargets) }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	_, sent := cp.lastHeartbeat()
	assert.False(t, sent, "a heartbeat without the Kubernetes version would be refused, so none is sent")
}

// fakeDeployer applies by returning err, or by blocking until ctx ends.
type fakeDeployer struct {
	err   error
	block bool
}

func (f *fakeDeployer) Name() string { return "helm" }

func (f *fakeDeployer) Apply(ctx context.Context, _ deploy.Assignment, progress deploy.Progress) (deploy.Applied, error) {
	progress(deploy.StepFetching, "pulling")
	if f.block {
		<-ctx.Done()
		return deploy.Applied{}, ctx.Err()
	}
	return deploy.Applied{Ref: map[string]any{"type": "helm"}}, f.err
}

func applyingAssignment(id string) apiclient.DeploymentAssignment {
	return apiclient.DeploymentAssignment{
		Deployment: apiclient.DeploymentSnapshot{Id: id, Status: apiclient.DeploymentSnapshotStatusApplying},
		Apply:      apiclient.ApplyAssignment{Deployer: "helm"},
	}
}

func TestExecutor(t *testing.T) {
	tests := []struct {
		name     string
		deployer *fakeDeployer
		status   apiclient.DeploymentSnapshotStatus
		want     []string
	}{
		{name: "an apply reports progress then progressing", deployer: &fakeDeployer{}, want: []string{"dep_1:fetching", "dep_1:applied"}},
		{name: "a failed apply reports failed", deployer: &fakeDeployer{err: deploy.Failf("no chart")}, want: []string{"dep_1:fetching", "dep_1:failed"}},
		{name: "only applying deployments are applied", deployer: &fakeDeployer{}, status: apiclient.DeploymentSnapshotStatusProgressing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &fakeControlPlane{assignments: []apiclient.DeploymentAssignment{applyingAssignment("dep_1")}}
			if tt.status != "" {
				cp.assignments[0].Deployment.Status = tt.status
			}
			log := slog.New(slog.DiscardHandler)
			exec := newExecutor(cp, map[string]deploy.Deployer{"helm": tt.deployer}, report.New(cp, log), log, 1)
			_, err := exec.poll(t.Context(), "prod-eu")
			require.NoError(t, err)
			exec.wait()
			assert.Equal(t, tt.want, cp.reportIDs())
			if len(tt.want) == 2 && tt.want[1] == "dep_1:failed" {
				assert.Equal(t, map[string]any{"reason": "no chart"}, cp.reports[1].Detail)
			}
		})
	}
}

func TestExecutor_ReportsNothingFinalWhenStoppedMidApply(t *testing.T) {
	cp := &fakeControlPlane{assignments: []apiclient.DeploymentAssignment{applyingAssignment("dep_1"), applyingAssignment("dep_2")}}
	log := slog.New(slog.DiscardHandler)
	exec := newExecutor(cp, map[string]deploy.Deployer{"helm": &fakeDeployer{block: true}}, report.New(cp, log), log, 1)
	ctx, cancel := context.WithCancel(t.Context())

	_, err := exec.poll(ctx, "prod-eu")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(cp.reportIDs()) == 1 }, 5*time.Second, 10*time.Millisecond)
	// One slot: the second Deployment waits for a later poll.
	_, err = exec.poll(ctx, "prod-eu")
	require.NoError(t, err)
	cancel()
	exec.wait()
	assert.Equal(t, []string{"dep_1:fetching"}, cp.reportIDs())
}
