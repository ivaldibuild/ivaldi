package enrollment

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type authorized struct {
	next  Service
	authz authorization.Authorizer
}

var _ Service = (*authorized)(nil)

// Create asks for cluster.manage on the Organization: a Cluster belongs to no
// Workspace, and minting its enrolment token hands out the ability to serve
// every Target in it.
func (a *authorized) Create(ctx context.Context, actor identity.Actor, in CreateInput) (*IssuedEnrolment, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionClusterManage, OrganizationID: in.OrganizationID,
		ResourceID: in.OrganizationID, Action: ActionClusterEnrol, Proposal: in,
	})
	if err != nil {
		return nil, err
	}
	return a.next.Create(authorization.WithDecision(ctx, "", d), actor, in)
}

func (a *authorized) Status(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*AgentStatus, error) {
	if _, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionClusterRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: ActionClusterAgentStatus,
	}); err != nil {
		return nil, err
	}
	return a.next.Status(ctx, actor, organizationID, clusterID)
}

// Redeem and Renew are authenticated by the workload, not by a grant, so they
// pass straight through: the service checks the token or credential.
func (a *authorized) Redeem(ctx context.Context, in RedeemInput) (*Redeemed, error) {
	return a.next.Redeem(ctx, in)
}

func (a *authorized) Renew(ctx context.Context, caller identity.Caller) (*Renewed, error) {
	return a.next.Renew(ctx, caller)
}
