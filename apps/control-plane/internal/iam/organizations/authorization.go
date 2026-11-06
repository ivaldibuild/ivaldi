package organizations

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Every operation is checked on the Organization's root resource:
// organization.read to read it, organization.manage to change it.
type authorized struct {
	next  iam.OrganizationService
	authz authorization.Authorizer
}

var _ iam.OrganizationService = (*authorized)(nil)

func (a *authorized) GetOrganization(ctx context.Context, actor identity.Actor, organizationID string) (*iam.Organization, error) {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionOrganizationRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: iam.ActionOrganizationRead,
	})
	if err != nil {
		return nil, err
	}
	return a.next.GetOrganization(ctx, actor, organizationID)
}

func (a *authorized) UpdateOrganization(ctx context.Context, actor identity.Actor, organizationID string, props iam.UpdateOrganizationProps) (*iam.Organization, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionOrganizationManage, OrganizationID: organizationID,
		ResourceID: organizationID, Action: iam.ActionOrganizationUpdate, Proposal: props,
	})
	if err != nil {
		return nil, err
	}
	return a.next.UpdateOrganization(authorization.WithDecision(ctx, "", d), actor, organizationID, props)
}
