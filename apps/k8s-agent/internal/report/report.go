// Package report sends the agent's reports about Deployments. Every report
// carries an ID the control plane stores once, so a retry, or a report
// repeated after a restart, is recorded once.
package report

import (
	"context"
	"log/slog"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// The report kinds.
const (
	KindProgress      = apiclient.ReportDeploymentRequestBodyKindProgress
	KindStatusChanged = apiclient.ReportDeploymentRequestBodyKindStatusChanged
	KindHealth        = apiclient.ReportDeploymentRequestBodyKindHealth
)

// Report is one report about a Deployment.
type Report struct {
	// Step makes the report ID, <deployment ID>:<step>; the same step is
	// recorded once.
	Step   string
	Kind   apiclient.ReportDeploymentRequestBodyKind
	Status apiclient.ReportDeploymentRequestBodyStatus // empty for none
	Detail map[string]any
}

// Reporter sends reports.
type Reporter interface {
	// Send sends r about the Deployment, retrying transient failures until
	// ctx ends.
	Send(ctx context.Context, deploymentID string, r Report) error
}

// New returns a Reporter over cp.
func New(cp apiclient.ControlPlane, log *slog.Logger) Reporter {
	return &reporter{cp: cp, log: log, now: time.Now, retryBase: time.Second}
}

type reporter struct {
	cp        apiclient.ControlPlane
	log       *slog.Logger
	now       func() time.Time
	retryBase time.Duration
}

// ID is the report ID of a step of a Deployment.
func ID(deploymentID, step string) string { return deploymentID + ":" + step }

func (r *reporter) Send(ctx context.Context, deploymentID string, rep Report) error {
	occurred := r.now().UTC()
	body := apiclient.ReportDeploymentRequestBody{
		ReportId:   ID(deploymentID, rep.Step),
		Kind:       rep.Kind,
		OccurredAt: &occurred,
	}
	if rep.Status != "" {
		body.Status = &rep.Status
	}
	if rep.Detail != nil {
		body.Detail = rep.Detail
	}
	wait := r.retryBase
	for {
		err := r.cp.ReportDeployment(ctx, deploymentID, body)
		if err == nil || !apiclient.Transient(err) || ctx.Err() != nil {
			if err != nil {
				r.log.Warn("could not report", "deployment_id", deploymentID, "report_id", body.ReportId, "error", err)
			}
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		wait = min(2*wait, 30*time.Second)
	}
}
