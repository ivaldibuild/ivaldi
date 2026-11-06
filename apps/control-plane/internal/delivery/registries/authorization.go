package registries

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Registries are not IAM resources: every operation is checked on the
// Registry's Workspace. Finding it for an existing Registry means reading
// the Registry first; next reads it again in its transaction, so a decision
// is never applied to stale state. A Registry never moves between
// Workspaces, so the Workspace read here is the one the change applies in.
type authorized struct {
	next       delivery.RegistryService
	authz      authorization.Authorizer
	registries delivery.RegistryRepository
}

var _ delivery.RegistryService = (*authorized)(nil)

// allow asks for a decision and returns ctx carrying it for the call.
func (a *authorized) allow(ctx context.Context, req authorization.Request) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, req)
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, workspaceID, action string) error {
	_, err := a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionRegistryRead, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: action,
	})
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, workspaceID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionRegistryManage, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: action, Proposal: proposal,
	})
}

// manageRegistry authorizes a change to a Registry on its Workspace.
func (a *authorized) manageRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID, action string, proposal any) (context.Context, error) {
	r, err := a.registries.GetRegistry(ctx, organizationID, registryID)
	if err != nil {
		return nil, err
	}
	return a.manage(ctx, actor, organizationID, r.WorkspaceID(), action, proposal)
}

func (a *authorized) CreateRegistry(ctx context.Context, actor identity.Actor, in delivery.CreateRegistryInput) (*delivery.Registry, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.WorkspaceID, delivery.ActionRegistryCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateRegistry(ctx, actor, in)
}

func (a *authorized) GetRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	r, err := a.next.GetRegistry(ctx, actor, organizationID, registryID)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, organizationID, r.WorkspaceID(), delivery.ActionRegistryRead); err != nil {
		return nil, err
	}
	return r, nil
}

func (a *authorized) ListRegistries(ctx context.Context, actor identity.Actor, query delivery.ListRegistriesQuery) (*pagination.Result[*delivery.Registry], error) {
	if err := a.read(ctx, actor, query.OrganizationID, query.WorkspaceID, delivery.ActionRegistryList); err != nil {
		return nil, err
	}
	return a.next.ListRegistries(ctx, actor, query)
}

func (a *authorized) UpdateRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string, props delivery.UpdateRegistryProps) (*delivery.Registry, error) {
	ctx, err := a.manageRegistry(ctx, actor, organizationID, registryID, delivery.ActionRegistryUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateRegistry(ctx, actor, organizationID, registryID, props)
}

func (a *authorized) SetRegistryCredential(ctx context.Context, actor identity.Actor, organizationID, registryID, credentialRef string) (*delivery.Registry, error) {
	ctx, err := a.manageRegistry(ctx, actor, organizationID, registryID, delivery.ActionRegistrySetCredential, credentialRef)
	if err != nil {
		return nil, err
	}
	return a.next.SetRegistryCredential(ctx, actor, organizationID, registryID, credentialRef)
}

func (a *authorized) SetRegistryLookup(ctx context.Context, actor identity.Actor, organizationID, registryID string, from delivery.LookupFrom, pool string) (*delivery.Registry, error) {
	proposal := struct {
		LookupFrom delivery.LookupFrom
		LookupPool string
	}{from, pool}
	ctx, err := a.manageRegistry(ctx, actor, organizationID, registryID, delivery.ActionRegistrySetLookup, proposal)
	if err != nil {
		return nil, err
	}
	return a.next.SetRegistryLookup(ctx, actor, organizationID, registryID, from, pool)
}

func (a *authorized) ArchiveRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	ctx, err := a.manageRegistry(ctx, actor, organizationID, registryID, delivery.ActionRegistryArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveRegistry(ctx, actor, organizationID, registryID)
}

func (a *authorized) RestoreRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	ctx, err := a.manageRegistry(ctx, actor, organizationID, registryID, delivery.ActionRegistryRestore, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreRegistry(ctx, actor, organizationID, registryID)
}
