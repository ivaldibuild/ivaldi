package targets

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

var (
	readRule = authorizationv1.ResourceRule{
		Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps"}, Resources: []string{"*"},
	}
	editRule = authorizationv1.ResourceRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}
)

// fakeKube answers permission reviews from rules per namespace, and access
// reviews from allowed.
func fakeKube(rules map[string][]authorizationv1.ResourceRule, incomplete bool, allowed func(authorizationv1.ResourceAttributes) bool) *fake.Clientset {
	kube := fake.NewClientset()
	kube.PrependReactor("create", "selfsubjectrulesreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectRulesReview)
		review.Status = authorizationv1.SubjectRulesReviewStatus{ResourceRules: rules[review.Spec.Namespace], Incomplete: incomplete}
		return true, review, nil
	})
	kube.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
		review.Status.Allowed = allowed != nil && allowed(*review.Spec.ResourceAttributes)
		return true, review, nil
	})
	kube.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.31.4"}
	return kube
}

func TestAccess_Namespace(t *testing.T) {
	tests := []struct {
		name       string
		rules      []authorizationv1.ResourceRule
		incomplete bool
		allowed    func(authorizationv1.ResourceAttributes) bool
		want       Capabilities
	}{
		{name: "read and edit", rules: []authorizationv1.ResourceRule{readRule, editRule}, want: Capabilities{Observe: true, Execute: true}},
		{name: "read only", rules: []authorizationv1.ResourceRule{readRule},
			want: Capabilities{Observe: true, Reason: "RBAC denies create on deployments.apps"}},
		{name: "nothing", want: Capabilities{Reason: "RBAC denies get on pods"}},
		{
			name: "a rule for named objects grants nothing",
			rules: []authorizationv1.ResourceRule{{
				Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}, ResourceNames: []string{"one"},
			}},
			want: Capabilities{Reason: "RBAC denies get on pods"},
		},
		{
			name: "an incomplete review asks about each denial", rules: []authorizationv1.ResourceRule{readRule}, incomplete: true,
			allowed: func(authorizationv1.ResourceAttributes) bool { return true },
			want:    Capabilities{Observe: true, Execute: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fakeKube(map[string][]authorizationv1.ResourceRule{"payments": tt.rules}, tt.incomplete, tt.allowed)
			got, err := NewAccess(kube).Namespace(t.Context(), "payments")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// fakeControlPlane serves a fixed Targets response.
type fakeControlPlane struct{ targets apiclient.TargetsResponse }

func (f *fakeControlPlane) Targets(context.Context) (*apiclient.TargetsResponse, error) {
	return &f.targets, nil
}
func (f *fakeControlPlane) Heartbeat(context.Context, apiclient.HeartbeatRequestBody) error {
	return nil
}

func (f *fakeControlPlane) Assignments(context.Context) ([]apiclient.DeploymentAssignment, error) {
	return nil, nil
}

func (f *fakeControlPlane) ReportDeployment(context.Context, string, apiclient.ReportDeploymentRequestBody) error {
	return nil
}

func (f *fakeControlPlane) Observe(context.Context, apiclient.ObservationRequestBody) (*apiclient.DeploymentSnapshot, error) {
	return &apiclient.DeploymentSnapshot{}, nil
}

func TestTracker_RefreshChecksAccessAndWatchesTheNamespacesItCanRead(t *testing.T) {
	kube := fakeKube(map[string][]authorizationv1.ResourceRule{
		"payments":  {readRule, editRule},
		"reporting": {readRule},
	}, false, nil)
	cp := &fakeControlPlane{targets: apiclient.TargetsResponse{
		Cluster:                  apiclient.ClusterView{Id: "cls_1", Slug: "prod-eu"},
		HeartbeatIntervalSeconds: 30, PollIntervalSeconds: 5, WatchIntervalSeconds: 60,
		Targets: []apiclient.TargetView{
			{Id: "tgt_1", Namespace: "payments"},
			{Id: "tgt_2", Namespace: "reporting"},
			{Id: "tgt_3", Namespace: "secret-ops"},
		},
	}}
	tr := NewTracker(cp, kube, slog.New(slog.DiscardHandler))
	defer tr.Stop()

	set, err := tr.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "prod-eu", set.Cluster.Slug)
	assert.Equal(t, "v1.31.4", set.KubernetesVersion)
	assert.Equal(t, WatchModeNamespaces, set.WatchMode)
	assert.Empty(t, set.Unsynced)
	require.Len(t, set.Targets, 3)
	assert.Equal(t, Capabilities{Observe: true, Execute: true}, set.Targets[0].Capabilities)
	assert.Equal(t, Capabilities{Observe: true, Reason: "RBAC denies create on deployments.apps"}, set.Targets[1].Capabilities)
	assert.False(t, set.Targets[2].Capabilities.Observe)
	assert.Equal(t, []string{"payments", "reporting"}, tr.(*tracker).informers.Watched())

	body := set.HeartbeatBody("1.0.0", []string{"helm"}, []string{"kubernetes"}, []string{"doppler-production"})
	assert.Equal(t, "v1.31.4", body.KubernetesVersion)
	assert.Equal(t, []string{"helm"}, *body.Deployers)
	assert.Equal(t, []string{"doppler-production"}, *body.SecretStoreCredentials)
	require.Len(t, *body.Targets, 3)
	assert.Equal(t, apiclient.TargetHeartbeatRequest{TargetId: "tgt_1", Observe: true, Execute: true}, (*body.Targets)[0])

	// A Target that goes away stops its informers.
	cp.targets.Targets = cp.targets.Targets[:1]
	_, err = tr.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"payments"}, tr.(*tracker).informers.Watched())
}
