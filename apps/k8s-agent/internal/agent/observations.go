package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/observe"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// observations reports what each Application runs on each Target: once
// after start-up, then whenever its digest set changes.
type observations struct {
	cp     apiclient.ControlPlane
	log    *slog.Logger
	now    func() time.Time
	listed func(namespace string) (targets.Lister, error)
	// sent is the last digest set reported per Target and Application.
	sent map[string]string
}

func newObservations(cp apiclient.ControlPlane, listed func(string) (targets.Lister, error), log *slog.Logger) *observations {
	return &observations{cp: cp, log: log, now: time.Now, listed: listed, sent: map[string]string{}}
}

// check observes every Application on every Target the agent can read. A
// namespace it cannot read is not observed: the heartbeat already says so.
// It returns the first error that says the agent no longer serves the
// Cluster.
func (o *observations) check(ctx context.Context, set targets.Set) error {
	for _, t := range set.Targets {
		if !t.Capabilities.Observe {
			continue
		}
		lister, err := o.listed(t.Namespace)
		if errors.Is(err, targets.ErrNotWatched) {
			continue
		}
		if err != nil {
			return err
		}
		for _, app := range t.Applications {
			selector := app.WorkloadSelector
			if len(selector) == 0 {
				selector = map[string]string{deploy.LabelApplication: app.Id}
			}
			seen, err := observe.Observe(lister, selector)
			if err != nil {
				o.log.Warn("could not observe", "target_id", t.Id, "application_id", app.Id, "error", err)
				continue
			}
			if len(seen.Images) == 0 {
				continue
			}
			key, slot := seen.Key(), t.Id+"/"+app.Id
			if o.sent[slot] == key {
				continue
			}
			err = o.cp.Observe(ctx, o.body(t.TargetView, app.Id, t.Namespace, key, seen))
			switch {
			case err == nil:
				o.sent[slot] = key
				o.log.Info("observed", "target_id", t.Id, "application_id", app.Id, "images", len(seen.Images))
			case errors.Is(err, apiclient.ErrNotClusterAgent):
				return err
			case apiclient.Transient(err):
				// Tried again at the next check.
			default:
				// Refused: retrying the same observation would be refused
				// again, so wait for the digests to change.
				o.sent[slot] = key
				o.log.Warn("the observation was refused", "target_id", t.Id, "application_id", app.Id, "error", err)
			}
		}
	}
	return nil
}

func (o *observations) body(t apiclient.TargetView, applicationID, namespace, key string, seen observe.Observation) apiclient.ObservationRequestBody {
	artifacts := make([]apiclient.ObservedArtifactRequest, 0, len(seen.Images))
	for _, image := range seen.Images {
		artifacts = append(artifacts, apiclient.ObservedArtifactRequest{ArtifactName: image.Repository, Digest: image.Digest})
	}
	workloads := make([]apiclient.ObservedWorkload, 0, len(seen.Workloads))
	for _, w := range seen.Workloads {
		workload := apiclient.ObservedWorkload{Kind: w.Kind, Name: w.Name, Namespace: namespace}
		if w.Revision != "" {
			workload.Revision = &w.Revision
		}
		workloads = append(workloads, workload)
	}
	observed := o.now().UTC()
	body := apiclient.ObservationRequestBody{
		ReportId:      "obs:" + t.Id + ":" + applicationID + ":" + key,
		TargetId:      t.Id,
		ApplicationId: applicationID,
		Artifacts:     artifacts,
		Workloads:     &workloads,
		ObservedAt:    &observed,
	}
	if seen.HelmRelease != "" {
		body.ExternalRef = map[string]any{"type": "helm", "name": seen.HelmRelease}
	}
	return body
}
