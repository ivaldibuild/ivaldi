package targets

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// DefaultHeartbeat is the heartbeat interval before the control plane has
// sent one, matching its default.
const DefaultHeartbeat = defaultHeartbeat

// The intervals the agent falls back to if the control plane sends none,
// matching the control plane's defaults.
const (
	defaultHeartbeat = 30 * time.Second
	defaultPoll      = 5 * time.Second
	defaultWatch     = 60 * time.Second
)

// syncTimeout bounds how long a refresh waits for new informers; one that
// has not synced keeps syncing and is waited on at the next refresh.
const syncTimeout = 20 * time.Second

// accessConcurrency bounds the permission reviews a refresh runs at once.
const accessConcurrency = 8

// Watch modes, as the heartbeat reports them.
const (
	WatchModeCluster    = "cluster"
	WatchModeNamespaces = "namespaces"
)

// Target is one of the Cluster's Targets with what the agent can do in its
// namespace.
type Target struct {
	apiclient.TargetView
	Capabilities Capabilities
}

// Set is what the agent serves, as of its last refresh.
type Set struct {
	Cluster   apiclient.ClusterView
	Heartbeat time.Duration
	Poll      time.Duration
	Watch     time.Duration
	Targets   []Target
	// WatchMode is cluster when RBAC lets the agent read every namespace.
	WatchMode         string
	KubernetesVersion string
	// Unsynced are namespaces whose informers have not synced yet.
	Unsynced []string
}

// SecretCapabilities is what the agent can read secrets with, for the
// heartbeat: never a credential, only credentials' names.
type SecretCapabilities struct {
	StoreKinds  []string
	StoreModes  []string
	Credentials []string
}

// HeartbeatBody is the heartbeat that reports s.
func (s Set) HeartbeatBody(agentVersion string, deployers []string, secrets SecretCapabilities) apiclient.HeartbeatRequestBody {
	list := func(values []string) *[]string {
		if values == nil {
			values = []string{}
		}
		return &values
	}
	reports := make([]apiclient.TargetHeartbeatRequest, 0, len(s.Targets))
	for _, t := range s.Targets {
		report := apiclient.TargetHeartbeatRequest{
			TargetId: t.Id, Observe: t.Capabilities.Observe, Execute: t.Capabilities.Execute,
		}
		if t.Capabilities.Reason != "" {
			report.Reason = &t.Capabilities.Reason
		}
		reports = append(reports, report)
	}
	watchMode := s.WatchMode
	if watchMode == "" {
		// Before the first refresh has checked RBAC.
		watchMode = WatchModeNamespaces
	}
	return apiclient.HeartbeatRequestBody{
		AgentVersion:           agentVersion,
		KubernetesVersion:      s.KubernetesVersion,
		WatchMode:              apiclient.HeartbeatRequestBodyWatchMode(watchMode),
		Deployers:              &deployers,
		SecretStoreKinds:       list(secrets.StoreKinds),
		SecretStoreModes:       list(secrets.StoreModes),
		SecretStoreCredentials: list(secrets.Credentials),
		Targets:                &reports,
	}
}

// Tracker keeps the agent's Set current.
type Tracker interface {
	// Refresh fetches the Targets, checks what the agent can do in each
	// namespace, and starts or stops informers to match. It keeps the
	// previous Set when the Targets cannot be fetched.
	Refresh(ctx context.Context) (Set, error)
	// Current returns the Set from the last successful Refresh.
	Current() Set
	// KubernetesVersion returns the cluster's version, reading it from
	// the API server when it has not been read yet.
	KubernetesVersion() (string, error)
	// Workloads returns an Application's workloads from the informer
	// caches; see Informers.Workloads.
	Workloads(namespace string, selector map[string]string) ([]*unstructured.Unstructured, error)
	// Lister reads a watched namespace from the informer caches.
	Lister(namespace string) (Lister, error)
	// Stop stops the informers.
	Stop()
}

// NewTracker returns a Tracker for the agent's Cluster.
func NewTracker(cp apiclient.ControlPlane, kube kubernetes.Interface, log *slog.Logger) Tracker {
	return &tracker{
		cp:        cp,
		access:    NewAccess(kube),
		informers: NewInformers(kube),
		version: func() (string, error) {
			info, err := kube.Discovery().ServerVersion()
			if err != nil {
				return "", err
			}
			return info.GitVersion, nil
		},
		log: log,
	}
}

type tracker struct {
	cp        apiclient.ControlPlane
	access    Access
	informers Informers
	version   func() (string, error)
	log       *slog.Logger

	mu      sync.Mutex
	current Set
	// kubeVersion is the last version read, kept when a later read fails.
	kubeVersion string
}

func (t *tracker) KubernetesVersion() (string, error) {
	t.mu.Lock()
	cached := t.kubeVersion
	t.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	return t.readVersion()
}

// readVersion reads the version from the API server and caches it.
func (t *tracker) readVersion() (string, error) {
	v, err := t.version()
	if err != nil {
		return "", fmt.Errorf("read the Kubernetes version: %w", err)
	}
	t.mu.Lock()
	t.kubeVersion = v
	t.mu.Unlock()
	return v, nil
}

func (t *tracker) Refresh(ctx context.Context) (Set, error) {
	resp, err := t.cp.Targets(ctx)
	if err != nil {
		return t.Current(), err
	}
	previous := t.Current()
	next := Set{
		Cluster:   resp.Cluster,
		Heartbeat: seconds(resp.HeartbeatIntervalSeconds, defaultHeartbeat),
		Poll:      seconds(resp.PollIntervalSeconds, defaultPoll),
		Watch:     seconds(resp.WatchIntervalSeconds, defaultWatch),
		WatchMode: WatchModeNamespaces,
	}
	// Read again at every refresh, so an upgraded cluster is reported;
	// a failed read keeps the version read before.
	if _, err := t.readVersion(); err != nil {
		t.log.Warn("could not read the Kubernetes version", "error", err)
	}
	next.KubernetesVersion, _ = t.KubernetesVersion()
	if wide, err := t.access.ClusterWide(ctx); err != nil {
		t.log.Warn("could not check cluster-wide access", "error", err)
	} else if wide {
		next.WatchMode = WatchModeCluster
	}

	next.Targets = make([]Target, len(resp.Targets))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(accessConcurrency)
	for i, view := range resp.Targets {
		group.Go(func() error {
			caps, err := t.access.Namespace(groupCtx, view.Namespace)
			if err != nil {
				t.log.Warn("could not check access", "target_id", view.Id, "namespace", view.Namespace, "error", err)
				caps = Capabilities{Reason: fmt.Sprintf("the agent could not check its permissions in %s", view.Namespace)}
			}
			next.Targets[i] = Target{TargetView: view, Capabilities: caps}
			return nil
		})
	}
	_ = group.Wait()
	if err := ctx.Err(); err != nil {
		return previous, err
	}

	var observable []string
	for _, target := range next.Targets {
		if target.Capabilities.Observe {
			observable = append(observable, target.Namespace)
		}
	}
	unsynced, err := t.informers.Watch(ctx, observable, syncTimeout)
	if ctx.Err() != nil {
		return previous, ctx.Err()
	}
	if err != nil {
		t.log.Warn("informers have not synced yet", "error", err)
	}
	next.Unsynced = unsynced

	t.mu.Lock()
	t.current = next
	t.mu.Unlock()
	return next, nil
}

func (t *tracker) Current() Set {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current
}

func (t *tracker) Stop() { t.informers.Stop() }

func (t *tracker) Lister(namespace string) (Lister, error) { return t.informers.Lister(namespace) }

func (t *tracker) Workloads(namespace string, selector map[string]string) ([]*unstructured.Unstructured, error) {
	return t.informers.Workloads(namespace, selector)
}

func seconds(n int64, fallback time.Duration) time.Duration {
	if n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}
