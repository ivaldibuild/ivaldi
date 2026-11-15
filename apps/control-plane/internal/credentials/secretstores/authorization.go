package secretstores

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Secret Stores are not IAM resources: every operation is checked on the
// store's Workspace. Finding it for an existing store means reading the
// store first; next reads it again in its transaction. A store never moves
// between Workspaces.
type authorized struct {
	next   credentials.SecretStoreService
	authz  authorization.Authorizer
	stores credentials.SecretStoreRepository
}

var _ credentials.SecretStoreService = (*authorized)(nil)

func (a *authorized) allow(ctx context.Context, actor identity.Actor, permission, organizationID, workspaceID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

// manageStore authorizes a change to a store on its Workspace.
func (a *authorized) manageStore(ctx context.Context, actor identity.Actor, organizationID, storeID, action string, proposal any) (context.Context, error) {
	store, err := a.stores.GetSecretStore(ctx, organizationID, storeID)
	if err != nil {
		return nil, err
	}
	return a.allow(ctx, actor, credentials.PermissionSecretStoreManage, organizationID, store.WorkspaceID(), action, proposal)
}

func (a *authorized) CreateSecretStore(ctx context.Context, actor identity.Actor, in credentials.CreateSecretStoreInput) (*credentials.SecretStore, error) {
	ctx, err := a.allow(ctx, actor, credentials.PermissionSecretStoreManage, in.OrganizationID, in.WorkspaceID, credentials.ActionSecretStoreCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateSecretStore(ctx, actor, in)
}

func (a *authorized) GetSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
	store, err := a.next.GetSecretStore(ctx, actor, organizationID, storeID)
	if err != nil {
		return nil, err
	}
	if _, err := a.allow(ctx, actor, credentials.PermissionSecretStoreRead, organizationID, store.WorkspaceID(), credentials.ActionSecretStoreRead, nil); err != nil {
		return nil, err
	}
	return store, nil
}

func (a *authorized) ListSecretStores(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*credentials.SecretStore, error) {
	if _, err := a.allow(ctx, actor, credentials.PermissionSecretStoreRead, organizationID, workspaceID, credentials.ActionSecretStoreList, nil); err != nil {
		return nil, err
	}
	return a.next.ListSecretStores(ctx, actor, organizationID, workspaceID, includeArchived)
}

func (a *authorized) UpdateSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string, props credentials.UpdateSecretStoreProps) (*credentials.SecretStore, error) {
	ctx, err := a.manageStore(ctx, actor, organizationID, storeID, credentials.ActionSecretStoreUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateSecretStore(ctx, actor, organizationID, storeID, props)
}

func (a *authorized) ArchiveSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
	ctx, err := a.manageStore(ctx, actor, organizationID, storeID, credentials.ActionSecretStoreArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveSecretStore(ctx, actor, organizationID, storeID)
}
