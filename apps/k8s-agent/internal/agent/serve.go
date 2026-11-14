package agent

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// retryCap bounds the wait between attempts at the first refresh.
const retryCap = 30 * time.Second

// work is everything the agent does while it holds the Lease: keep the
// credential, keep the Targets and informers current, and heartbeat. It
// returns nil when ctx ends, and auth.ErrCredentialLost when the credential
// is gone.
func (a *agent) work(ctx context.Context, st state.State) error {
	a.log.Info("working", "cluster_id", st.ClusterID, "organization_id", st.OrganizationID)
	tracker := a.newTracker()
	defer tracker.Stop()
	defer a.ready.set(conditionTargets, false)

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return a.creds.Maintain(ctx) })
	group.Go(func() error {
		refreshNow, beatNow := make(chan struct{}, 1), make(chan struct{}, 1)
		loops, ctx := errgroup.WithContext(ctx)
		// Liveness does not wait for the Targets: a slow or failing first
		// refresh must not let the Cluster go stale.
		loops.Go(func() error { a.heartbeatLoop(ctx, tracker, refreshNow, beatNow); return nil })
		if !a.firstRefresh(ctx, tracker) {
			return loops.Wait()
		}
		// Report the Targets' capabilities as soon as they are known.
		beatNow <- struct{}{}
		loops.Go(func() error { a.refreshLoop(ctx, tracker, refreshNow); return nil })
		return loops.Wait()
	})
	return group.Wait()
}

// firstRefresh fetches the Targets, retrying until it succeeds, and reports
// whether ctx is still live.
func (a *agent) firstRefresh(ctx context.Context, tracker targets.Tracker) bool {
	wait := a.retryBase
	for {
		if err := a.refresh(ctx, tracker); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait):
		}
		wait = min(2*wait, retryCap)
	}
}

// heartbeatLoop heartbeats at once, then at the control plane's interval,
// or the default one until the first refresh brings it. Before the first
// refresh a heartbeat names no Targets. A heartbeat needs the Kubernetes
// version, so one that cannot be read is skipped rather than sent invalid.
// A heartbeat refused as invalid, because a Target it names is no longer
// active, asks for a refresh, after which the next heartbeat is correct.
func (a *agent) heartbeatLoop(ctx context.Context, tracker targets.Tracker, refreshNow chan<- struct{}, beatNow <-chan struct{}) {
	for {
		set := tracker.Current()
		if set.Heartbeat <= 0 {
			set.Heartbeat = targets.DefaultHeartbeat
		}
		version, verr := tracker.KubernetesVersion()
		set.KubernetesVersion = version
		var err error
		if verr != nil {
			err = verr
		} else {
			err = a.cp.Heartbeat(ctx, set.HeartbeatBody(a.version))
		}
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			a.log.Debug("heartbeat", "targets", len(set.Targets))
		case errors.Is(err, apiclient.ErrInvalid):
			a.log.Warn("the heartbeat was refused; refreshing the targets", "error", err)
			select {
			case refreshNow <- struct{}{}:
			default:
			}
		default:
			a.handle(err, "heartbeat failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(set.Heartbeat):
		case <-beatNow:
		}
	}
}

// refreshLoop refreshes the Targets at the control plane's watch interval,
// or when asked.
func (a *agent) refreshLoop(ctx context.Context, tracker targets.Tracker, refreshNow <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(tracker.Current().Watch):
		case <-refreshNow:
		}
		_ = a.refresh(ctx, tracker)
	}
}

// refresh refreshes the Targets and sets readiness from their informers.
func (a *agent) refresh(ctx context.Context, tracker targets.Tracker) error {
	set, err := tracker.Refresh(ctx)
	if err != nil {
		if ctx.Err() == nil {
			a.handle(err, "could not fetch the targets")
		}
		return err
	}
	a.ready.set(conditionTargets, len(set.Unsynced) == 0)
	a.log.Debug("refreshed the targets", "cluster", set.Cluster.Slug, "targets", len(set.Targets),
		"watch_mode", set.WatchMode, "unsynced", set.Unsynced)
	return nil
}

// handle logs a failed control-plane call, and gives up the credential when
// the control plane says this agent no longer serves the Cluster.
func (a *agent) handle(err error, msg string) {
	if errors.Is(err, apiclient.ErrNotClusterAgent) {
		a.creds.Invalidate("the control plane says this agent no longer serves the cluster; another agent may have been enrolled")
		return
	}
	a.log.Warn(msg, "error", err)
}
