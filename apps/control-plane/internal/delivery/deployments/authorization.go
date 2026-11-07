package deployments

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. A Deployment request asks per Target,
// because each Target's Environment is its own resource.
type authorized struct {
	next         delivery.DeploymentService
	authz        authorization.Authorizer
	releases     Releases
	targets      Targets
	applications Applications
	deployments  delivery.DeploymentRepository
}

var _ delivery.DeploymentService = (*authorized)(nil)

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, action, organizationID, resourceID string, proposal any) (authorization.Decision, error) {
	d, err := a.authz.Authorize(ctx, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: resourceID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return authorization.Decision{}, err
	}
	switch d.Outcome {
	case authorization.OutcomeAllow, authorization.OutcomeRequiresApproval:
		return d, nil
	default:
		return d, &authorization.DeniedError{DecisionID: d.ID, Reason: d.Reason}
	}
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, action, organizationID, applicationID string) error {
	app, err := a.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return err
	}
	_, err = authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionDeploymentRead, OrganizationID: organizationID,
		ResourceID: app.ProjectID(), Action: action,
	})
	return err
}

func (a *authorized) RequestDeployments(ctx context.Context, actor identity.Actor, in delivery.RequestDeploymentsInput) ([]*delivery.Deployment, error) {
	rel, err := a.releases.GetRelease(ctx, in.OrganizationID, in.ReleaseID)
	if err != nil {
		return nil, err
	}
	app, err := a.applications.GetApplication(ctx, in.OrganizationID, rel.ApplicationID())
	if err != nil {
		return nil, err
	}
	for _, targetID := range in.TargetIDs {
		target, err := a.targets.GetTarget(ctx, in.OrganizationID, targetID)
		if err != nil {
			return nil, err
		}
		d, err := a.decide(ctx, actor, delivery.PermissionDeploymentCreate, delivery.ActionDeploymentCreate, in.OrganizationID, target.EnvironmentID(), in)
		if err != nil {
			return nil, err
		}
		ctx = authorization.WithDecision(ctx, targetID, d)
		if _, err := authorization.Require(ctx, a.authz, authorization.Request{
			Actor: actor, Permission: delivery.PermissionApplicationRead, OrganizationID: in.OrganizationID,
			ResourceID: app.ProjectID(), Action: delivery.ActionDeploymentCreate,
		}); err != nil {
			return nil, err
		}
	}
	return a.next.RequestDeployments(ctx, actor, in)
}

func (a *authorized) GetDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentDetail, error) {
	d, err := a.deployments.GetDeployment(ctx, organizationID, deploymentID)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, delivery.ActionDeploymentRead, organizationID, d.ApplicationID()); err != nil {
		return nil, err
	}
	return a.next.GetDeployment(ctx, actor, organizationID, deploymentID)
}

func (a *authorized) ListDeployments(ctx context.Context, actor identity.Actor, query delivery.ListDeploymentsQuery) (*pagination.Result[delivery.DeploymentSnapshot], error) {
	app, err := a.applications.GetApplicationByIdentifier(ctx, query.OrganizationID, query.ApplicationID)
	if err != nil {
		return nil, err
	}
	query.ApplicationID = app.ID()
	if err := a.read(ctx, actor, delivery.ActionDeploymentList, query.OrganizationID, query.ApplicationID); err != nil {
		return nil, err
	}
	return a.next.ListDeployments(ctx, actor, query)
}

func (a *authorized) CancelDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentDetail, error) {
	d, err := a.deployments.GetDeployment(ctx, organizationID, deploymentID)
	if err != nil {
		return nil, err
	}
	target, err := a.targets.GetTarget(ctx, organizationID, d.TargetID())
	if err != nil {
		return nil, err
	}
	decision, err := a.decide(ctx, actor, delivery.PermissionDeploymentCreate, delivery.ActionDeploymentCancel, organizationID, target.EnvironmentID(), deploymentID)
	if err != nil {
		return nil, err
	}
	ctx = authorization.WithDecision(ctx, deploymentID, decision)
	return a.next.CancelDeployment(ctx, actor, organizationID, deploymentID)
}

func (a *authorized) ListAssignments(ctx context.Context, actor identity.Actor, organizationID, targetID string) ([]delivery.DeploymentAssignment, error) {
	return a.next.ListAssignments(ctx, actor, organizationID, targetID)
}

func (a *authorized) ReportDeployment(ctx context.Context, actor identity.Actor, in delivery.ReportDeploymentInput) (*delivery.DeploymentDetail, error) {
	return a.next.ReportDeployment(ctx, actor, in)
}

func (a *authorized) RecordTargetHeartbeat(ctx context.Context, actor identity.Actor, organizationID, targetID string) error {
	return a.next.RecordTargetHeartbeat(ctx, actor, organizationID, targetID)
}

func (a *authorized) ReleaseChanges(ctx context.Context, actor identity.Actor, organizationID, releaseID, targetID string) (*delivery.ReleaseChanges, error) {
	rel, err := a.releases.GetRelease(ctx, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, delivery.ActionDeploymentChanges, organizationID, rel.ApplicationID()); err != nil {
		return nil, err
	}
	return a.next.ReleaseChanges(ctx, actor, organizationID, releaseID, targetID)
}
