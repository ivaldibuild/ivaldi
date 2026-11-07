package enrollment

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type authorized struct {
	next         Service
	authz        authorization.Authorizer
	targets      Targets
	environments Environments
}

var _ Service = (*authorized)(nil)

func (a *authorized) Create(ctx context.Context, actor identity.Actor, in CreateInput) (*IssuedEnrolment, error) {
	target, err := a.targets.GetTarget(ctx, in.OrganizationID, in.TargetID)
	if err != nil {
		return nil, err
	}
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionEnvironmentManage, OrganizationID: in.OrganizationID,
		ResourceID: target.EnvironmentID(), Action: "target.enrol", Proposal: in,
	})
	if err != nil {
		return nil, err
	}
	return a.next.Create(authorization.WithDecision(ctx, "", d), actor, in)
}

func (a *authorized) Status(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*AgentStatus, error) {
	target, err := a.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	if _, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionEnvironmentRead, OrganizationID: organizationID,
		ResourceID: target.EnvironmentID(), Action: "target.agent_status",
	}); err != nil {
		return nil, err
	}
	return a.next.Status(ctx, actor, organizationID, targetID)
}

func (a *authorized) Redeem(ctx context.Context, in RedeemInput) (*Redeemed, error) {
	return a.next.Redeem(ctx, in)
}

func (a *authorized) Renew(ctx context.Context, caller identity.Caller) (*Renewed, error) {
	return a.next.Renew(ctx, caller)
}
