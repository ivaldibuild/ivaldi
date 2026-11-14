package observe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

// fakeLister serves fixed objects; selectors filter workloads only.
type fakeLister struct {
	deployments  []*appsv1.Deployment
	statefulSets []*appsv1.StatefulSet
	daemonSets   []*appsv1.DaemonSet
	replicaSets  []*appsv1.ReplicaSet
	revisions    []*appsv1.ControllerRevision
	pods         []*corev1.Pod
}

func matching[T metav1.Object](items []T, s labels.Selector) []T {
	var out []T
	for _, item := range items {
		if s.Matches(labels.Set(item.GetLabels())) {
			out = append(out, item)
		}
	}
	return out
}

func (f fakeLister) Deployments(s labels.Selector) ([]*appsv1.Deployment, error) {
	return matching(f.deployments, s), nil
}
func (f fakeLister) StatefulSets(s labels.Selector) ([]*appsv1.StatefulSet, error) {
	return matching(f.statefulSets, s), nil
}
func (f fakeLister) DaemonSets(s labels.Selector) ([]*appsv1.DaemonSet, error) {
	return matching(f.daemonSets, s), nil
}
func (f fakeLister) ReplicaSets() ([]*appsv1.ReplicaSet, error) { return f.replicaSets, nil }
func (f fakeLister) ControllerRevisions() ([]*appsv1.ControllerRevision, error) {
	return f.revisions, nil
}
func (f fakeLister) Pods() ([]*corev1.Pod, error) { return f.pods, nil }

var app = map[string]string{"ivaldi.build/application": "app_1"}

func meta(name string, uid types.UID, owner types.UID, lbls, annotations map[string]string) metav1.ObjectMeta {
	m := metav1.ObjectMeta{Name: name, UID: uid, Labels: lbls, Annotations: annotations}
	if owner != "" {
		m.OwnerReferences = []metav1.OwnerReference{{UID: owner}}
	}
	return m
}

// pod runs image; resolved is what the kubelet reports.
func pod(name string, owner types.UID, lbls map[string]string, image, resolved string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: meta(name, types.UID(name), owner, lbls, nil),
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: image}}},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "main", ImageID: resolved}}},
	}
}

func TestObserve_Deployment_ReadsOnlyTheCurrentReplicaSet(t *testing.T) {
	l := fakeLister{
		deployments: []*appsv1.Deployment{{ObjectMeta: meta("api", "d1", "", app, map[string]string{
			"deployment.kubernetes.io/revision": "2", "meta.helm.sh/release-name": "payments",
		})}},
		replicaSets: []*appsv1.ReplicaSet{
			{ObjectMeta: meta("api-old", "rs1", "d1", map[string]string{"pod-template-hash": "old"}, map[string]string{"deployment.kubernetes.io/revision": "1"})},
			{ObjectMeta: meta("api-new", "rs2", "d1", map[string]string{"pod-template-hash": "new"}, map[string]string{"deployment.kubernetes.io/revision": "2"})},
		},
		pods: []*corev1.Pod{
			pod("api-old-1", "rs1", map[string]string{"pod-template-hash": "old"}, "ghcr.io/acme/api:1.0", "docker-pullable://ghcr.io/acme/api@sha256:old"),
			pod("api-new-1", "rs2", map[string]string{"pod-template-hash": "new"}, "ghcr.io/acme/api:1.1", "ghcr.io/acme/api@sha256:new"),
		},
	}

	got, err := Observe(l, app)
	require.NoError(t, err)
	assert.Equal(t, []Image{{Repository: "ghcr.io/acme/api", Digest: "sha256:new"}}, got.Images)
	assert.Equal(t, []Workload{{Kind: "Deployment", Name: "api", Revision: "new"}}, got.Workloads)
	assert.Equal(t, "payments", got.HelmRelease)
}

func TestObserve_StatefulSet_MidRolloutReadsTheUpdateRevision(t *testing.T) {
	sts := &appsv1.StatefulSet{ObjectMeta: meta("db", "s1", "", app, nil)}
	sts.Status.UpdateRevision, sts.Status.CurrentRevision = "db-2", "db-1"
	l := fakeLister{
		statefulSets: []*appsv1.StatefulSet{sts},
		pods: []*corev1.Pod{
			pod("db-0", "s1", map[string]string{"controller-revision-hash": "db-1"}, "postgres:16", "postgres@sha256:one"),
			pod("db-1", "s1", map[string]string{"controller-revision-hash": "db-2"}, "postgres:17", "postgres@sha256:two"),
		},
	}
	got, err := Observe(l, app)
	require.NoError(t, err)
	assert.Equal(t, []Image{{Repository: "postgres", Digest: "sha256:two"}}, got.Images)
}

func TestObserve_DaemonSet_ReadsTheNewestControllerRevision(t *testing.T) {
	l := fakeLister{
		daemonSets: []*appsv1.DaemonSet{{ObjectMeta: meta("agent", "ds1", "", app, nil)}},
		revisions: []*appsv1.ControllerRevision{
			{ObjectMeta: meta("agent-a", "cr1", "ds1", map[string]string{"controller-revision-hash": "a"}, nil), Revision: 1},
			{ObjectMeta: meta("agent-b", "cr2", "ds1", map[string]string{"controller-revision-hash": "b"}, nil), Revision: 2},
		},
		pods: []*corev1.Pod{
			pod("agent-x", "ds1", map[string]string{"controller-revision-hash": "a"}, "ghcr.io/acme/agent:1", "ghcr.io/acme/agent@sha256:a"),
			// A digest in the spec wins over what the kubelet reports.
			pod("agent-y", "ds1", map[string]string{"controller-revision-hash": "b"}, "ghcr.io/acme/agent@sha256:spec", "ghcr.io/acme/agent@sha256:status"),
		},
	}
	got, err := Observe(l, app)
	require.NoError(t, err)
	assert.Equal(t, []Image{{Repository: "ghcr.io/acme/agent", Digest: "sha256:spec"}}, got.Images)
	assert.Equal(t, "2", got.Workloads[0].Revision)
}

func TestObserve_CountsInitContainersAndSkipsUnstartedOnes(t *testing.T) {
	p := pod("api-1", "rs1", map[string]string{"pod-template-hash": "h"}, "ghcr.io/acme/api:1", "")
	p.Spec.InitContainers = []corev1.Container{{Name: "migrate", Image: "ghcr.io/acme/migrate:1"}}
	p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "migrate", ImageID: "ghcr.io/acme/migrate@sha256:m"}}
	l := fakeLister{
		deployments: []*appsv1.Deployment{{ObjectMeta: meta("api", "d1", "", app, map[string]string{"deployment.kubernetes.io/revision": "1"})}},
		replicaSets: []*appsv1.ReplicaSet{{ObjectMeta: meta("api-h", "rs1", "d1", map[string]string{"pod-template-hash": "h"}, map[string]string{"deployment.kubernetes.io/revision": "1"})}},
		pods:        []*corev1.Pod{p},
	}
	got, err := Observe(l, app)
	require.NoError(t, err)
	assert.Equal(t, []Image{{Repository: "ghcr.io/acme/migrate", Digest: "sha256:m"}}, got.Images)
}

func TestSplitImage(t *testing.T) {
	tests := []struct{ ref, repository, digest string }{
		{"ghcr.io/acme/api:1.0", "ghcr.io/acme/api", ""},
		{"ghcr.io/acme/api@sha256:abc", "ghcr.io/acme/api", "sha256:abc"},
		{"ghcr.io/acme/api:1.0@sha256:abc", "ghcr.io/acme/api", "sha256:abc"},
		{"registry:5000/api:1.0", "registry:5000/api", ""},
		{"registry:5000/api", "registry:5000/api", ""},
		{"docker-pullable://nginx@sha256:abc", "nginx", "sha256:abc"},
		{"sha256:abc", "", "sha256:abc"},
	}
	for _, tt := range tests {
		repository, digest := splitImage(tt.ref)
		assert.Equal(t, tt.repository, repository, tt.ref)
		assert.Equal(t, tt.digest, digest, tt.ref)
	}
}

func TestKey_IgnoresOrder(t *testing.T) {
	a := Observation{Images: []Image{{"x", "sha256:1"}, {"y", "sha256:2"}}}
	b := Observation{Images: []Image{{"y", "sha256:2"}, {"x", "sha256:1"}}}
	assert.Equal(t, a.Key(), b.Key())
	assert.Len(t, a.Key(), 16)
}
