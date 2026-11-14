// Package observe finds what an Application is running: the image digests
// of the current revision of each of its workloads, read from Pods, whoever
// deployed them. Old revisions still draining during a rollout are ignored.
package observe

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// The labels and annotations Kubernetes and Helm set that observation reads.
const (
	labelPodTemplateHash      = "pod-template-hash"
	labelControllerRevision   = "controller-revision-hash"
	annotationDeploymentRev   = "deployment.kubernetes.io/revision"
	annotationHelmReleaseName = "meta.helm.sh/release-name"
)

// Image is one image the agent saw running, by digest.
type Image struct {
	Repository string
	Digest     string
}

// Workload is a workload the images came from.
type Workload struct {
	Kind     string
	Name     string
	Revision string
}

// Observation is what an Application runs in one namespace.
type Observation struct {
	Images    []Image
	Workloads []Workload
	// HelmRelease names the Helm release that deployed the workloads, when
	// they say so.
	HelmRelease string
}

// Key identifies the observation's digest set: equal keys mean nothing
// changed.
func (o Observation) Key() string {
	refs := make([]string, 0, len(o.Images))
	for _, image := range o.Images {
		refs = append(refs, image.Repository+"@"+image.Digest)
	}
	slices.Sort(refs)
	sum := sha256.Sum256([]byte(strings.Join(refs, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// Observe returns what the workloads matching selector run. A workload with
// no running Pod of its current revision contributes nothing yet.
func Observe(l targets.Lister, selector map[string]string) (Observation, error) {
	match := labels.SelectorFromSet(selector)
	pods, err := l.Pods()
	if err != nil {
		return Observation{}, err
	}
	var out Observation
	add := func(kind string, meta metav1.Object, revision string, current []*corev1.Pod) {
		if len(current) == 0 {
			return
		}
		out.Workloads = append(out.Workloads, Workload{Kind: kind, Name: meta.GetName(), Revision: revision})
		if name := meta.GetAnnotations()[annotationHelmReleaseName]; name != "" && out.HelmRelease == "" {
			out.HelmRelease = name
		}
		for _, pod := range current {
			for _, image := range podImages(pod) {
				if !slices.Contains(out.Images, image) {
					out.Images = append(out.Images, image)
				}
			}
		}
	}

	deployments, err := l.Deployments(match)
	if err != nil {
		return Observation{}, err
	}
	replicaSets, err := l.ReplicaSets()
	if err != nil {
		return Observation{}, err
	}
	for _, d := range deployments {
		// The current ReplicaSet carries the Deployment's revision.
		revision := d.Annotations[annotationDeploymentRev]
		for _, rs := range replicaSets {
			if ownedBy(rs.OwnerReferences, d.UID) && revision != "" && rs.Annotations[annotationDeploymentRev] == revision {
				hash := rs.Labels[labelPodTemplateHash]
				add("Deployment", d, hash, owned(pods, rs.UID, labelPodTemplateHash, hash))
			}
		}
	}

	statefulSets, err := l.StatefulSets(match)
	if err != nil {
		return Observation{}, err
	}
	for _, s := range statefulSets {
		// The template being rolled out, or the current one when no
		// update is in progress.
		revision := s.Status.UpdateRevision
		if revision == "" {
			revision = s.Status.CurrentRevision
		}
		add("StatefulSet", s, revision, owned(pods, s.UID, labelControllerRevision, revision))
	}

	daemonSets, err := l.DaemonSets(match)
	if err != nil {
		return Observation{}, err
	}
	revisions, err := l.ControllerRevisions()
	if err != nil {
		return Observation{}, err
	}
	for _, d := range daemonSets {
		// DaemonSets record no revision in status: the newest
		// ControllerRevision they own is the current one.
		var newest *appsv1.ControllerRevision
		for _, cr := range revisions {
			if ownedBy(cr.OwnerReferences, d.UID) && (newest == nil || cr.Revision > newest.Revision) {
				newest = cr
			}
		}
		if newest == nil {
			continue
		}
		hash := newest.Labels[labelControllerRevision]
		add("DaemonSet", d, strconv.FormatInt(newest.Revision, 10), owned(pods, d.UID, labelControllerRevision, hash))
	}
	slices.SortFunc(out.Images, func(a, b Image) int {
		return strings.Compare(a.Repository+a.Digest, b.Repository+b.Digest)
	})
	return out, nil
}

// owned returns the Pods owner owns whose label key equals value.
func owned(pods []*corev1.Pod, owner types.UID, key, value string) []*corev1.Pod {
	if value == "" {
		return nil
	}
	var out []*corev1.Pod
	for _, pod := range pods {
		if ownedBy(pod.OwnerReferences, owner) && pod.Labels[key] == value {
			out = append(out, pod)
		}
	}
	return out
}

func ownedBy(refs []metav1.OwnerReference, uid types.UID) bool {
	return slices.ContainsFunc(refs, func(r metav1.OwnerReference) bool { return r.UID == uid })
}

// podImages returns the images a Pod runs, init containers included, each by
// digest: the spec's digest when it names one, otherwise the digest the
// kubelet resolved. A container that has not started is skipped.
func podImages(pod *corev1.Pod) []Image {
	specs := map[string]string{}
	for _, c := range append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers...) {
		specs[c.Name] = c.Image
	}
	var out []Image
	for _, status := range append(slices.Clone(pod.Status.InitContainerStatuses), pod.Status.ContainerStatuses...) {
		spec, ok := specs[status.Name]
		if !ok || !started(status) {
			continue
		}
		repository, digest := splitImage(spec)
		if digest == "" {
			_, digest = splitImage(status.ImageID)
		}
		if digest == "" {
			continue
		}
		out = append(out, Image{Repository: repository, Digest: digest})
	}
	return out
}

// started reports whether a container has run: it is running, or has
// terminated now or before, as a completed init container has. A container
// still waiting, such as one in ImagePullBackOff, has run nothing, whatever
// its spec names.
func started(s corev1.ContainerStatus) bool {
	return s.State.Running != nil || s.State.Terminated != nil || s.LastTerminationState.Terminated != nil
}

// splitImage splits an image reference into its repository, without tag,
// and its sha256 digest, if it names one. It accepts the kubelet's imageID
// forms too, such as docker-pullable://repo@sha256:….
func splitImage(ref string) (repository, digest string) {
	if _, rest, ok := strings.Cut(ref, "://"); ok {
		ref = rest
	}
	if name, d, ok := strings.Cut(ref, "@"); ok {
		ref = name
		if strings.HasPrefix(d, "sha256:") {
			digest = d
		}
	} else if strings.HasPrefix(ref, "sha256:") {
		return "", ref
	}
	// A tag follows the last colon after the last slash; a colon before it
	// is a registry port.
	if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		ref = ref[:colon]
	}
	return ref, digest
}
