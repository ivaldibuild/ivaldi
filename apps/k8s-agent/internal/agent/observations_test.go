package agent

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// oneStatefulSet is a namespace with one StatefulSet running image.
type oneStatefulSet struct{ image string }

func (o *oneStatefulSet) Deployments(labels.Selector) ([]*appsv1.Deployment, error) { return nil, nil }
func (o *oneStatefulSet) DaemonSets(labels.Selector) ([]*appsv1.DaemonSet, error)   { return nil, nil }
func (o *oneStatefulSet) ReplicaSets() ([]*appsv1.ReplicaSet, error)                { return nil, nil }
func (o *oneStatefulSet) ControllerRevisions() ([]*appsv1.ControllerRevision, error) {
	return nil, nil
}
func (o *oneStatefulSet) StatefulSets(labels.Selector) ([]*appsv1.StatefulSet, error) {
	s := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", UID: "s1"}}
	s.Status.CurrentRevision = "db-1"
	return []*appsv1.StatefulSet{s}, nil
}
func (o *oneStatefulSet) Pods() ([]*corev1.Pod, error) {
	return []*corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "db-0", Labels: map[string]string{"controller-revision-hash": "db-1"},
			OwnerReferences: []metav1.OwnerReference{{UID: "s1"}}},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: o.image}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "db", ImageID: o.image}}},
	}}, nil
}

func TestObservations_ReportOnceAndAgainWhenTheDigestsChange(t *testing.T) {
	ns := &oneStatefulSet{image: "postgres@sha256:one"}
	cp := &fakeControlPlane{}
	o := newObservations(cp, func(string) (targets.Lister, error) { return ns, nil }, slog.New(slog.DiscardHandler))
	set := targets.Set{Targets: []targets.Target{
		{TargetView: apiclient.TargetView{Id: "tgt_1", Namespace: "data", Applications: []apiclient.ApplicationView{{Id: "app_1"}}},
			Capabilities: targets.Capabilities{Observe: true}},
		{TargetView: apiclient.TargetView{Id: "tgt_2", Namespace: "hidden", Applications: []apiclient.ApplicationView{{Id: "app_1"}}}},
	}}

	require.NoError(t, o.check(t.Context(), set))
	require.NoError(t, o.check(t.Context(), set))
	require.Len(t, cp.observations, 1, "the namespace it cannot observe is skipped, and an unchanged one is sent once")
	first := cp.observations[0]
	assert.Equal(t, "tgt_1", first.TargetId)
	assert.Equal(t, []apiclient.ObservedArtifactRequest{{ArtifactName: "postgres", Digest: "sha256:one"}}, first.Artifacts)
	assert.Regexp(t, `^obs:tgt_1:app_1:[0-9a-f]{16}$`, first.ReportId)

	ns.image = "postgres@sha256:two"
	require.NoError(t, o.check(t.Context(), set))
	require.Len(t, cp.observations, 2)
	assert.NotEqual(t, first.ReportId, cp.observations[1].ReportId)
}
