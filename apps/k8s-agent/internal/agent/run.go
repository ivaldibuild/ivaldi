package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/auth"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
)

// errLeaseLost ends the process: another replica may now be applying, so
// this one must stop and start over.
var errLeaseLost = errors.New("lost the lease")

// The Lease timings client-go recommends for controllers.
const (
	leaseDuration = 15 * time.Second
	renewDeadline = 10 * time.Second
	retryPeriod   = 2 * time.Second
)

// enrolRetry is how often an agent without a credential looks for a new
// enrolment token, and how long it waits after a failure it cannot name.
const enrolRetry = 30 * time.Second

type agent struct {
	cfg        Config
	log        *slog.Logger
	kube       kubernetes.Interface
	creds      auth.Credentials
	ready      *readiness
	enrolRetry time.Duration
}

// run enrols, takes the Lease, and works until ctx ends. Without a
// credential it waits, not ready, for a new enrolment token rather than
// exiting, so a Pod that needs one does not crash-loop.
func (a *agent) run(ctx context.Context) error {
	waiting := false
	for {
		st, err := a.creds.Enrol(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, auth.ErrNotEnrolled):
			if !waiting {
				a.log.Warn("waiting for an enrolment token", "secret", a.cfg.EnrolmentSecret, "namespace", a.cfg.Namespace)
				waiting = true
			}
			if !a.sleep(ctx) {
				return nil
			}
			continue
		case err != nil:
			a.log.Error("could not enrol; retrying", "error", err, "in", a.enrolRetry)
			if !a.sleep(ctx) {
				return nil
			}
			continue
		}
		waiting = false
		a.ready.set(conditionEnrolled, true)
		err = a.lead(ctx, st)
		a.ready.set(conditionEnrolled, false)
		if errors.Is(err, auth.ErrCredentialLost) {
			continue
		}
		return err
	}
}

// lead holds the Lease and works while it does. It returns nil when ctx
// ends, errLeaseLost when the Lease is lost, and otherwise what the work
// returned, such as auth.ErrCredentialLost.
func (a *agent) lead(ctx context.Context, st state.State) error {
	leadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var workErr error
	started, workDone := make(chan struct{}), make(chan struct{})
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta:  metav1.ObjectMeta{Name: a.cfg.LeaseName, Namespace: a.cfg.Namespace},
			Client:     a.kube.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{Identity: a.cfg.PodName},
		},
		Name:            a.cfg.LeaseName,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(workCtx context.Context) {
				close(started)
				defer close(workDone)
				a.ready.set(conditionLease, true)
				workErr = a.work(workCtx, st)
				// Release the Lease when the work stops on its own.
				cancel()
			},
			OnStoppedLeading: func() { a.ready.set(conditionLease, false) },
		},
	})
	if err != nil {
		return err
	}
	a.log.Info("waiting for the lease", "lease", a.cfg.LeaseName, "identity", a.cfg.PodName)
	elector.Run(leadCtx)

	select {
	case <-started:
		// Losing the Lease cancels the work; let it finish its report.
		<-workDone
	default:
		// ctx ended before the Lease was taken.
		return nil
	}
	switch {
	case workErr != nil:
		return workErr
	case ctx.Err() != nil:
		return nil
	default:
		return errLeaseLost
	}
}

// work is everything the agent does while it holds the Lease. It returns
// nil when ctx ends.
func (a *agent) work(ctx context.Context, st state.State) error {
	a.log.Info("working", "cluster_id", st.ClusterID, "organization_id", st.OrganizationID)
	return a.creds.Maintain(ctx)
}

// sleep waits enrolRetry and reports whether ctx is still live.
func (a *agent) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(a.enrolRetry):
		return true
	}
}
