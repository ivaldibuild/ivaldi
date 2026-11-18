package delivery

import (
	"context"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SubjectDeployment is the approval subject type of a Deployment that waits
// for approval, registered with policy.
const SubjectDeployment = "delivery.deployment"

// DeploymentApprovalSummary is what approvers of a Deployment are shown: a
// snapshot of what was asked, kept with the approval request.
type DeploymentApprovalSummary struct {
	ApplicationID   string           `json:"application_id"`
	ReleaseID       string           `json:"release_id"`
	ReleaseVersion  string           `json:"release_version"`
	EnvironmentID   string           `json:"environment_id"`
	EnvironmentTier Tier             `json:"environment_tier" enum:"development,staging,production,other"`
	TargetID        string           `json:"target_id"`
	Reason          DeploymentReason `json:"reason" enum:"rollout,rollback,redeploy"`
	// RestoresDeploymentID is the Deployment a rollback replays.
	RestoresDeploymentID *string `json:"restores_deployment_id"`
	// SecretsLatest says a rollback reads the latest secrets rather than
	// the versions the Deployment it restores used.
	SecretsLatest bool `json:"secrets_latest"`
}

// WaitingDeployments names the Deployments awaiting approval that something
// stops from ever being applied: those of a withdrawn Release, or on an
// archived Target, or of an archived Application. Set one of the IDs.
type WaitingDeployments struct {
	OrganizationID string
	ReleaseID      string
	TargetID       string
	ApplicationID  string
}

// Cause is the ID of what stops the Deployments, whose decision the
// cancellations are recorded under.
func (w WaitingDeployments) Cause() string {
	switch {
	case w.ReleaseID != "":
		return w.ReleaseID
	case w.TargetID != "":
		return w.TargetID
	}
	return w.ApplicationID
}

// WaitingDeploymentCanceller cancels Deployments awaiting approval, with
// their approval requests, when they can no longer be applied. It does not
// authorize: the caller withdrew the Release or archived the Target or
// Application, in its transaction, whose context carries the decision for
// that object. Deployments already applying are left alone.
type WaitingDeploymentCanceller interface {
	CancelWaitingDeployments(ctx context.Context, actor identity.Actor, w WaitingDeployments, note string) error
}
