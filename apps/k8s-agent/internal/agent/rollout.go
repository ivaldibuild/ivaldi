package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ivaldibuild/ivaldi/pkg/release"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy/status"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// degradedDebounce is how long a healthy Deployment's workloads must stay
// unhealthy before it is reported degraded.
const degradedDebounce = 60 * time.Second

// emptyGrace is how long a progressing Deployment with no workloads waits
// before it counts as healthy, so a just-applied chart's workloads have
// reached the informer caches.
const emptyGrace = 30 * time.Second

// healthReportTimeout bounds one health report, retries included.
const healthReportTimeout = 10 * time.Second

// deploymentsWorkloads returns the workloads to judge a Deployment by, and
// whether the caches reflect it. Ivaldi annotates everything it applies with
// the Deployment that applied it: when any workload names another
// Deployment, the caches have not caught up yet; when some name this one,
// only those count. Workloads Ivaldi did not apply carry no annotation, so
// an observed Deployment is judged by all of them.
func deploymentsWorkloads(objects []*unstructured.Unstructured, deploymentID string) ([]*unstructured.Unstructured, bool) {
	var ours []*unstructured.Unstructured
	for _, obj := range objects {
		switch applied, ok := obj.GetAnnotations()[deploy.AnnotationDeployment]; {
		case !ok:
		case applied == deploymentID:
			ours = append(ours, obj)
		default:
			return nil, false
		}
	}
	if len(ours) > 0 {
		return ours, true
	}
	return objects, true
}

// watched is one Deployment whose health the agent tracks.
type watched struct {
	deploymentID string
	namespace    string
	selector     map[string]string
	// known is the status the control plane gave, or "" for a placement's
	// current Deployment, whose status the agent does not get.
	known release.DeploymentStatus
}

// rollouts reports the health of the Deployments the agent tracks: those
// still progressing, and each placement's current Deployment, whoever
// applied it.
type rollouts struct {
	source   workloadSource
	reporter report.Reporter
	health   *status.Tracker
	log      *slog.Logger
	now      func() time.Time
	// reportTimeout bounds one health report.
	reportTimeout time.Duration
}

// workloadSource reads an Application's workloads; targets.Tracker is one.
type workloadSource interface {
	Workloads(namespace string, selector map[string]string) ([]*unstructured.Unstructured, error)
}

func newRollouts(source workloadSource, reporter report.Reporter, log *slog.Logger) *rollouts {
	return &rollouts{
		source: source, reporter: reporter, log: log, now: time.Now,
		health:        status.NewTracker(degradedDebounce, emptyGrace),
		reportTimeout: healthReportTimeout,
	}
}

// toWatch lists the Deployments to track: the progressing assignments, and
// each placement's current Deployment.
func toWatch(assignments []apiclient.DeploymentAssignment, set targets.Set) []watched {
	selectors := map[string]map[string]string{}
	var out []watched
	seen := map[string]bool{}
	for _, t := range set.Targets {
		for _, app := range t.Applications {
			selectors[t.Id+"/"+app.Id] = app.WorkloadSelector
		}
	}
	selector := func(targetID, applicationID string) map[string]string {
		if s := selectors[targetID+"/"+applicationID]; len(s) > 0 {
			return s
		}
		return map[string]string{deploy.LabelApplication: applicationID}
	}
	for _, a := range assignments {
		if a.Deployment.Status != apiclient.DeploymentSnapshotStatusProgressing {
			continue
		}
		seen[a.Deployment.Id] = true
		out = append(out, watched{
			deploymentID: a.Deployment.Id, namespace: a.Apply.Namespace,
			selector: selector(a.Target.Id, a.Deployment.ApplicationId), known: release.DeploymentProgressing,
		})
	}
	for _, t := range set.Targets {
		for _, app := range t.Applications {
			if app.CurrentDeploymentId == nil || seen[*app.CurrentDeploymentId] {
				continue
			}
			seen[*app.CurrentDeploymentId] = true
			item := watched{deploymentID: *app.CurrentDeploymentId, namespace: t.Namespace, selector: selector(t.Id, app.Id)}
			if app.CurrentDeploymentStatus != nil {
				// The control plane's status, so an agent that restarts
				// carries on from degraded rather than assuming healthy.
				item.known = release.DeploymentStatus(*app.CurrentDeploymentStatus)
			}
			out = append(out, item)
		}
	}
	return out
}

// check evaluates each Deployment and reports the moves. A namespace the
// agent cannot read yet is skipped until it can.
func (r *rollouts) check(ctx context.Context, items []watched) {
	keep := map[string]bool{}
	for _, item := range items {
		keep[item.deploymentID] = true
		objects, err := r.source.Workloads(item.namespace, item.selector)
		if errors.Is(err, targets.ErrNotWatched) {
			continue
		}
		if err != nil {
			r.log.Warn("could not read workloads", "deployment_id", item.deploymentID, "error", err)
			continue
		}
		objects, current := deploymentsWorkloads(objects, item.deploymentID)
		if !current {
			// The caches still hold another Deployment's workloads, as
			// right after an apply: judging them would report this one
			// healthy or failed on the strength of the old revision.
			continue
		}
		overall, detail := status.Evaluate(objects)
		now := r.now()
		next := r.health.Next(item.deploymentID, item.known, overall, len(objects), now)
		if next == "" {
			continue
		}
		step := string(next)
		if item.known != release.DeploymentProgressing {
			// A Deployment may go healthy and degraded many times; each
			// move is its own report.
			step = fmt.Sprintf("%s:%d", next, now.Unix())
		}
		// Bounded, so a control plane that keeps failing cannot hold up the
		// assignment poll; a report that does not get through is tried
		// again at the next check.
		reportCtx, cancel := context.WithTimeout(ctx, r.reportTimeout)
		err = r.reporter.Send(reportCtx, item.deploymentID, report.Report{
			Step: step, Kind: report.KindStatusChanged,
			Status: apiclient.ReportDeploymentRequestBodyStatus(next),
			Detail: map[string]any{"workloads": detail},
		})
		cancel()
		if err == nil {
			r.health.Moved(item.deploymentID, next)
			r.log.Info("deployment status changed", "deployment_id", item.deploymentID, "status", next)
		} else if !apiclient.Transient(err) {
			// The control plane refused the move, so it knows a status the
			// agent does not; start over from what the next poll says.
			r.health.Forget(item.deploymentID)
		}
	}
	r.health.Keep(keep)
}
