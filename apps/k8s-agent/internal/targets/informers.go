package targets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// ErrNotWatched means a namespace's workloads are not in the caches: it is
// not watched, or its informers have not synced.
var ErrNotWatched = errors.New("the namespace is not watched")

// Informers keeps one informer set per watched namespace. It watches only
// Target namespaces, even when RBAC allows reading the whole cluster, so the
// agent caches nothing it does not report on.
type Informers interface {
	// Watch starts informers for namespaces not yet watched, stops those
	// for namespaces no longer listed, and waits until every watched one
	// has synced or timeout passes. It returns the namespaces that have not
	// synced, which keep syncing in the background and are waited on again
	// by the next Watch.
	Watch(ctx context.Context, namespaces []string, timeout time.Duration) (unsynced []string, err error)
	// Watched returns the watched namespaces, sorted.
	Watched() []string
	// Workloads returns the Deployments, StatefulSets, and DaemonSets in
	// namespace whose labels match selector, from the informer caches. It
	// returns ErrNotWatched when namespace is not watched or not synced.
	Workloads(namespace string, selector map[string]string) ([]*unstructured.Unstructured, error)
	// Lister reads namespace from the caches, or returns ErrNotWatched
	// when it is not watched or not synced.
	Lister(namespace string) (Lister, error)
	// Stop stops every informer.
	Stop()
}

// NewInformers returns Informers over kube.
func NewInformers(kube kubernetes.Interface) Informers {
	return &namespaceInformers{kube: kube, running: map[string]*running{}}
}

type namespaceInformers struct {
	kube    kubernetes.Interface
	mu      sync.Mutex
	running map[string]*running
}

type running struct {
	factory informers.SharedInformerFactory
	stop    chan struct{}
}

func (n *namespaceInformers) Watch(ctx context.Context, namespaces []string, timeout time.Duration) ([]string, error) {
	n.mu.Lock()
	wanted := map[string]bool{}
	for _, ns := range namespaces {
		wanted[ns] = true
		if _, ok := n.running[ns]; ok {
			continue
		}
		factory := informers.NewSharedInformerFactoryWithOptions(n.kube, 0, informers.WithNamespace(ns))
		// Asking for an informer registers it with the factory.
		factory.Core().V1().Pods().Informer()
		factory.Apps().V1().ReplicaSets().Informer()
		factory.Apps().V1().Deployments().Informer()
		factory.Apps().V1().StatefulSets().Informer()
		factory.Apps().V1().DaemonSets().Informer()
		factory.Apps().V1().ControllerRevisions().Informer()
		r := &running{factory: factory, stop: make(chan struct{})}
		factory.Start(r.stop)
		n.running[ns] = r
	}
	for ns, r := range n.running {
		if !wanted[ns] {
			r.halt()
			delete(n.running, ns)
		}
	}
	// Every watched namespace, not only the new ones, so one that missed
	// the last timeout is waited on again; a synced one returns at once.
	waiting := map[string]*running{}
	for ns, r := range n.running {
		waiting[ns] = r
	}
	n.mu.Unlock()

	syncCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var unsynced []string
	for ns, r := range waiting {
		for _, ok := range r.factory.WaitForCacheSync(syncCtx.Done()) {
			if !ok {
				unsynced = append(unsynced, ns)
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Strings(unsynced)
	if len(unsynced) > 0 {
		return unsynced, fmt.Errorf("informers did not sync within %s in: %v", timeout, unsynced)
	}
	return nil, nil
}

func (n *namespaceInformers) Watched() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.running))
	for ns := range n.running {
		out = append(out, ns)
	}
	slices.Sort(out)
	return out
}

func (n *namespaceInformers) Workloads(namespace string, selector map[string]string) ([]*unstructured.Unstructured, error) {
	n.mu.Lock()
	r, ok := n.running[namespace]
	n.mu.Unlock()
	if !ok {
		return nil, ErrNotWatched
	}
	apps := r.factory.Apps().V1()
	for _, informer := range []cache.SharedIndexInformer{apps.Deployments().Informer(), apps.StatefulSets().Informer(), apps.DaemonSets().Informer()} {
		if !informer.HasSynced() {
			return nil, ErrNotWatched
		}
	}
	match := labels.SelectorFromSet(selector)
	var objects []runtime.Object
	deployments, err := apps.Deployments().Lister().Deployments(namespace).List(match)
	if err != nil {
		return nil, err
	}
	for _, d := range deployments {
		objects = append(objects, withKind(d.DeepCopy(), appsv1.SchemeGroupVersion.WithKind("Deployment")))
	}
	statefulSets, err := apps.StatefulSets().Lister().StatefulSets(namespace).List(match)
	if err != nil {
		return nil, err
	}
	for _, s := range statefulSets {
		objects = append(objects, withKind(s.DeepCopy(), appsv1.SchemeGroupVersion.WithKind("StatefulSet")))
	}
	daemonSets, err := apps.DaemonSets().Lister().DaemonSets(namespace).List(match)
	if err != nil {
		return nil, err
	}
	for _, d := range daemonSets {
		objects = append(objects, withKind(d.DeepCopy(), appsv1.SchemeGroupVersion.WithKind("DaemonSet")))
	}

	out := make([]*unstructured.Unstructured, 0, len(objects))
	for _, obj := range objects {
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}
		out = append(out, &unstructured.Unstructured{Object: content})
	}
	return out, nil
}

// Lister reads one watched namespace's objects from the informer caches.
type Lister interface {
	Deployments(selector labels.Selector) ([]*appsv1.Deployment, error)
	StatefulSets(selector labels.Selector) ([]*appsv1.StatefulSet, error)
	DaemonSets(selector labels.Selector) ([]*appsv1.DaemonSet, error)
	ReplicaSets() ([]*appsv1.ReplicaSet, error)
	ControllerRevisions() ([]*appsv1.ControllerRevision, error)
	Pods() ([]*corev1.Pod, error)
}

func (n *namespaceInformers) Lister(namespace string) (Lister, error) {
	n.mu.Lock()
	r, ok := n.running[namespace]
	n.mu.Unlock()
	if !ok {
		return nil, ErrNotWatched
	}
	core, apps := r.factory.Core().V1(), r.factory.Apps().V1()
	for _, informer := range []cache.SharedIndexInformer{
		core.Pods().Informer(), apps.ReplicaSets().Informer(), apps.Deployments().Informer(),
		apps.StatefulSets().Informer(), apps.DaemonSets().Informer(), apps.ControllerRevisions().Informer(),
	} {
		if !informer.HasSynced() {
			return nil, ErrNotWatched
		}
	}
	return namespaceLister{factory: r.factory, namespace: namespace}, nil
}

type namespaceLister struct {
	factory   informers.SharedInformerFactory
	namespace string
}

func (l namespaceLister) Deployments(selector labels.Selector) ([]*appsv1.Deployment, error) {
	return l.factory.Apps().V1().Deployments().Lister().Deployments(l.namespace).List(selector)
}

func (l namespaceLister) StatefulSets(selector labels.Selector) ([]*appsv1.StatefulSet, error) {
	return l.factory.Apps().V1().StatefulSets().Lister().StatefulSets(l.namespace).List(selector)
}

func (l namespaceLister) DaemonSets(selector labels.Selector) ([]*appsv1.DaemonSet, error) {
	return l.factory.Apps().V1().DaemonSets().Lister().DaemonSets(l.namespace).List(selector)
}

func (l namespaceLister) ReplicaSets() ([]*appsv1.ReplicaSet, error) {
	return l.factory.Apps().V1().ReplicaSets().Lister().ReplicaSets(l.namespace).List(labels.Everything())
}

func (l namespaceLister) ControllerRevisions() ([]*appsv1.ControllerRevision, error) {
	return l.factory.Apps().V1().ControllerRevisions().Lister().ControllerRevisions(l.namespace).List(labels.Everything())
}

func (l namespaceLister) Pods() ([]*corev1.Pod, error) {
	return l.factory.Core().V1().Pods().Lister().Pods(l.namespace).List(labels.Everything())
}

// withKind sets the type information informers leave empty, which status
// computation needs.
func withKind(obj runtime.Object, gvk schema.GroupVersionKind) runtime.Object {
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	return obj
}

func (n *namespaceInformers) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ns, r := range n.running {
		r.halt()
		delete(n.running, ns)
	}
}

func (r *running) halt() {
	close(r.stop)
	r.factory.Shutdown()
}
