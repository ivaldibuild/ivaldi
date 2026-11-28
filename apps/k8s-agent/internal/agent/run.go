package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/auth"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/secrets"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
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
	version    string
	log        *slog.Logger
	kube       kubernetes.Interface
	creds      auth.Credentials
	cp         apiclient.ControlPlane
	newTracker func() targets.Tracker
	deployers  map[string]deploy.Deployer
	reporter   report.Reporter
	// secrets is nil in tests that use no secrets.
	secrets secrets.Resolver
	// secretCapabilities are the secret store kinds and modes this agent's
	// configuration lets it use, and the credentials its operator
	// configured, which the heartbeat reports.
	secretCapabilities targets.SecretCapabilities
	secretWriter       secrets.Writer
	masker             *secrets.Masker
	ready              *readiness
	enrolRetry         time.Duration
	retryBase          time.Duration
}

// run takes the Lease, enrols, and works until ctx ends. Credentials are
// loaded and changed only while the Lease is held, and reloaded at each
// acquisition, so two replicas never redeem or rewrite them at once and a
// replica taking over never uses a stale copy. Without a credential it
// releases the Lease and waits, not ready, for a new enrolment token
// rather than exiting, so a Pod that needs one does not crash-loop.
func (a *agent) run(ctx context.Context) error {
	waiting := false
	for {
		err := a.lead(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, auth.ErrNotEnrolled):
			if !waiting {
				a.log.Warn("waiting for an enrolment token", "secret", a.cfg.EnrolmentSecret, "namespace", a.cfg.Namespace)
				waiting = true
			}
		case errors.Is(err, errEnrol):
			a.log.Error("could not enrol; retrying", "error", err, "in", a.enrolRetry)
		case errors.Is(err, auth.ErrCredentialLost):
			// The next acquisition finds no credential and waits.
			waiting = false
			continue
		default:
			return err
		}
		if !a.sleep(ctx) {
			return nil
		}
	}
}

// errEnrol is an enrolment failure worth retrying, such as the state Secret
// being unreadable for a moment.
var errEnrol = errors.New("enrolment failed")

// lead holds the Lease, enrols, and works while it holds it. It returns
// nil when ctx ends, errLeaseLost when the Lease is lost, and otherwise
// why the work stopped, such as auth.ErrNotEnrolled or
// auth.ErrCredentialLost; the Lease is released either way.
func (a *agent) lead(ctx context.Context) error {
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
				workErr = a.enrolAndWork(workCtx)
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

// enrolAndWork enrols, reloading the state Secret, and works while ctx
// lasts.
func (a *agent) enrolAndWork(ctx context.Context) error {
	st, err := a.creds.Enrol(ctx)
	switch {
	case ctx.Err() != nil:
		return nil
	case errors.Is(err, auth.ErrNotEnrolled):
		return err
	case err != nil:
		return fmt.Errorf("%w: %w", errEnrol, err)
	}
	a.ready.set(conditionEnrolled, true)
	defer a.ready.set(conditionEnrolled, false)
	return a.work(ctx, st)
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
