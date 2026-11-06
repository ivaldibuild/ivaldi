package grants

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Every operation is checked on the grant's resource: grant.read to list
// grants, and grant.manage to create or remove one, so a role reaches only
// as far as the granter's own. The owner role needs owner.manage instead,
// which only owners hold. The authorizer refuses Agent Sessions, which may
// not change grants without a human's approval.
type authorized struct {
	next   iam.GrantService
	grants iam.GrantRepository
	authz  authorization.Authorizer
}

var _ iam.GrantService = (*authorized)(nil)

// managePermission is the permission granting or removing role needs.
func managePermission(roleKey string) string {
	if roleKey == iam.RoleOwner {
		return iam.PermissionOwnerManage
	}
	return iam.PermissionGrantManage
}

func (a *authorized) ListGrants(ctx context.Context, actor identity.Actor, q iam.ListResourceGrantsQuery) (*pagination.Result[iam.GrantEntry], error) {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionGrantRead, OrganizationID: q.OrganizationID,
		ResourceID: q.ResourceID, Action: iam.ActionGrantList,
	})
	if err != nil {
		return nil, err
	}
	return a.next.ListGrants(ctx, actor, q)
}

func (a *authorized) CreateGrant(ctx context.Context, actor identity.Actor, in iam.CreateGrantInput) (*iam.GrantEntry, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: managePermission(in.Role), OrganizationID: in.OrganizationID,
		ResourceID: in.ResourceID, Action: iam.ActionGrantCreate, Proposal: in,
	})
	if err != nil {
		return nil, err
	}
	return a.next.CreateGrant(authorization.WithDecision(ctx, "", d), actor, in)
}

// RemoveGrant reads the grant first, to ask about its resource and role.
func (a *authorized) RemoveGrant(ctx context.Context, actor identity.Actor, organizationID, grantID string) error {
	g, err := a.grants.Get(ctx, organizationID, grantID)
	if err != nil {
		return err
	}
	key, _ := iam.BuiltInRoleKey(g.RoleID())
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: managePermission(key), OrganizationID: organizationID,
		ResourceID: g.ResourceID(), Action: iam.ActionGrantRemove, Proposal: g.Snapshot(),
	})
	if err != nil {
		return err
	}
	return a.next.RemoveGrant(authorization.WithDecision(ctx, "", d), actor, organizationID, grantID)
}
