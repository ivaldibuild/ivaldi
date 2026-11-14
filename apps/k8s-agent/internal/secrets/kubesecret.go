package secrets

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// labelDeployment names, on a Deployment's Secret, the Deployment.
const labelDeployment = "ivaldi.build/deployment"

// Writer keeps the Kubernetes Secrets that deliver a Deployment's secrets.
type Writer interface {
	// Write creates the Deployment's immutable Secret holding every secret
	// delivered as a Secret, keyed by variable name, and returns its name.
	// An existing one, from an earlier attempt, is kept.
	Write(ctx context.Context, namespace, applicationID, deploymentID string, resolved []Resolved) (string, error)
	// Prune deletes the Application's Secrets in namespace except the
	// current Deployment's and the newest other one, whose Pods may still be
	// draining.
	Prune(ctx context.Context, namespace, applicationID, currentDeploymentID string) error
}

// NewWriter returns a Writer.
func NewWriter(kube kubernetes.Interface) Writer { return &writer{kube: kube} }

type writer struct{ kube kubernetes.Interface }

// SecretName is the name of a Deployment's Secret.
func SecretName(applicationID, deploymentID string) string {
	name := strings.ToLower(strings.ReplaceAll("ivaldi-"+applicationID+"-"+deploymentID, "_", "-"))
	return strings.Trim(name[:min(len(name), 253)], "-.")
}

func (w *writer) Write(ctx context.Context, namespace, applicationID, deploymentID string, resolved []Resolved) (string, error) {
	data := map[string][]byte{}
	for _, r := range resolved {
		if !r.AsValue {
			data[r.Name] = []byte(r.Value)
		}
	}
	if len(data) == 0 {
		return "", nil
	}
	name := SecretName(applicationID, deploymentID)
	immutable := true
	_, err := w.kube.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{deploy.LabelApplication: applicationID, labelDeployment: deploymentID},
		},
		Type: corev1.SecretTypeOpaque, Data: data, Immutable: &immutable,
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", &deploy.Failure{Reason: fmt.Sprintf("could not create Secret %s in %s", name, namespace), Err: err}
	}
	return name, nil
}

func (w *writer) Prune(ctx context.Context, namespace, applicationID, currentDeploymentID string) error {
	secrets := w.kube.CoreV1().Secrets(namespace)
	list, err := secrets.List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{deploy.LabelApplication: applicationID}).String() + "," + labelDeployment,
	})
	if err != nil {
		return err
	}
	items := list.Items
	slices.SortFunc(items, func(a, b corev1.Secret) int {
		return b.CreationTimestamp.Compare(a.CreationTimestamp.Time)
	})
	keptPrevious := false
	for _, s := range items {
		switch {
		case s.Labels[labelDeployment] == currentDeploymentID:
		case !keptPrevious:
			keptPrevious = true
		default:
			if err := secrets.Delete(ctx, s.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}
