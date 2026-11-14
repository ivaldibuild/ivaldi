package targets

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

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
