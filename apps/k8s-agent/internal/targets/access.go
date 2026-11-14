// Package targets tracks what the agent serves: its Cluster's Targets, what
// RBAC lets it do in each Target's namespace, and the informers that watch
// the namespaces it can read.
package targets

import (
	"context"
	"fmt"
	"slices"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Capabilities is what the agent can do in one namespace. Reason names the
// first permission missing when either capability is false.
type Capabilities struct {
	Observe bool
	Execute bool
	Reason  string
}

// Access finds what RBAC lets the agent do.
type Access interface {
	// Namespace returns the agent's capabilities in namespace.
	Namespace(ctx context.Context, namespace string) (Capabilities, error)
	// ClusterWide reports whether the agent may read the observed
	// resources in every namespace.
	ClusterWide(ctx context.Context) (bool, error)
}

// permission is one verb on one resource.
type permission struct {
	verb     string
	group    string
	resource string
}

func (p permission) String() string {
	if p.group == "" {
		return p.verb + " on " + p.resource
	}
	return p.verb + " on " + p.resource + "." + p.group
}

// observed are the resources observation reads.
var observed = []struct{ group, resource string }{
	{"", "pods"},
	{"apps", "replicasets"},
	{"apps", "deployments"},
	{"apps", "statefulsets"},
	{"apps", "daemonsets"},
}

// observePermissions is what observing a namespace needs.
var observePermissions = func() []permission {
	var out []permission
	for _, r := range observed {
		for _, verb := range []string{"get", "list", "watch"} {
			out = append(out, permission{verb, r.group, r.resource})
		}
	}
	return out
}()

// executePermissions is a sample of what applying a chart needs: the common
// workload objects, and Secrets, where Helm keeps its release history. The
// built-in edit and admin ClusterRoles grant all of it.
var executePermissions = func() []permission {
	var out []permission
	for _, r := range []struct{ group, resource string }{{"apps", "deployments"}, {"", "services"}, {"", "configmaps"}, {"", "secrets"}} {
		for _, verb := range []string{"get", "list", "create", "update", "patch", "delete"} {
			out = append(out, permission{verb, r.group, r.resource})
		}
	}
	return out
}()

// NewAccess returns Access checked with the Kubernetes authorization API:
// one SelfSubjectRulesReview per namespace, with a SelfSubjectAccessReview
// for any denial the rules review cannot be sure of.
func NewAccess(kube kubernetes.Interface) Access {
	return &access{kube: kube}
}

type access struct {
	kube kubernetes.Interface
}

func (a *access) Namespace(ctx context.Context, namespace string) (Capabilities, error) {
	review, err := a.kube.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
	}, metav1.CreateOptions{})
	if err != nil {
		return Capabilities{}, fmt.Errorf("review the agent's permissions in %s: %w", namespace, err)
	}
	allowed := func(p permission) (bool, error) {
		if rulesAllow(review.Status.ResourceRules, p) {
			return true, nil
		}
		if !review.Status.Incomplete {
			return false, nil
		}
		// Another authorizer may allow what RBAC's rules do not show.
		return a.allowed(ctx, namespace, p)
	}

	out := Capabilities{Observe: true, Execute: true}
	for _, set := range []struct {
		permissions []permission
		capability  *bool
	}{{observePermissions, &out.Observe}, {executePermissions, &out.Execute}} {
		for _, p := range set.permissions {
			ok, err := allowed(p)
			if err != nil {
				return Capabilities{}, err
			}
			if !ok {
				*set.capability = false
				if out.Reason == "" {
					out.Reason = "RBAC denies " + p.String()
				}
				break
			}
		}
	}
	return out, nil
}

func (a *access) ClusterWide(ctx context.Context) (bool, error) {
	for _, r := range observed {
		for _, verb := range []string{"list", "watch"} {
			ok, err := a.allowed(ctx, "", permission{verb, r.group, r.resource})
			if err != nil || !ok {
				return false, err
			}
		}
	}
	return true, nil
}

// allowed asks the API server whether the agent holds p in namespace, or in
// every namespace when namespace is empty.
func (a *access) allowed(ctx context.Context, namespace string, p permission) (bool, error) {
	review, err := a.kube.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
			Namespace: namespace, Verb: p.verb, Group: p.group, Resource: p.resource,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("check %s in %q: %w", p, namespace, err)
	}
	return review.Status.Allowed, nil
}

// rulesAllow reports whether rules grant p on every object of its resource.
// A rule limited to named objects does not.
func rulesAllow(rules []authorizationv1.ResourceRule, p permission) bool {
	return slices.ContainsFunc(rules, func(r authorizationv1.ResourceRule) bool {
		return len(r.ResourceNames) == 0 &&
			matches(r.Verbs, p.verb) && matches(r.APIGroups, p.group) && matches(r.Resources, p.resource)
	})
}

func matches(values []string, want string) bool {
	return slices.Contains(values, "*") || slices.Contains(values, want)
}
