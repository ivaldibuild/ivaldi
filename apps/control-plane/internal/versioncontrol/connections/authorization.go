package connections

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Connections are not IAM resources, so every operation is checked on the
// Organization: connection.read to read, connection.manage to change.
type authorized struct {
	next  versioncontrol.ConnectionService
	authz authorization.Authorizer
}

var _ versioncontrol.ConnectionService = (*authorized)(nil)

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, organizationID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, action string) error {
	_, err := a.decide(ctx, actor, versioncontrol.PermissionConnectionRead, organizationID, action, nil)
	return err
}

func (a *authorized) CreateConnection(ctx context.Context, actor identity.Actor, in versioncontrol.CreateConnectionInput) (*versioncontrol.Connection, error) {
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionConnectionManage, in.OrganizationID, versioncontrol.ActionConnectionCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateConnection(ctx, actor, in)
}

func (a *authorized) GetConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	if err := a.read(ctx, actor, organizationID, versioncontrol.ActionConnectionRead); err != nil {
		return nil, err
	}
	return a.next.GetConnection(ctx, actor, organizationID, connectionID)
}

func (a *authorized) ListConnections(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*versioncontrol.Connection, error) {
	if err := a.read(ctx, actor, organizationID, versioncontrol.ActionConnectionList); err != nil {
		return nil, err
	}
	return a.next.ListConnections(ctx, actor, organizationID, includeArchived)
}

func (a *authorized) ArchiveConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionConnectionManage, organizationID, versioncontrol.ActionConnectionArchive, connectionID)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveConnection(ctx, actor, organizationID, connectionID)
}

func (a *authorized) StartInstallation(ctx context.Context, actor identity.Actor, organizationID string, provider vcs.ProviderKind) (*versioncontrol.InstallationStart, error) {
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionConnectionManage, organizationID, versioncontrol.ActionConnectionInstall, provider)
	if err != nil {
		return nil, err
	}
	return a.next.StartInstallation(ctx, actor, organizationID, provider)
}

// CompleteInstallation proposes the provider and installation, never the
// authorization code or the state, which are credentials.
func (a *authorized) CompleteInstallation(ctx context.Context, actor identity.Actor, in versioncontrol.CompleteInstallationInput) (*versioncontrol.Connection, error) {
	proposal := struct {
		Provider       vcs.ProviderKind `json:"provider"`
		InstallationID string           `json:"installation_id"`
	}{in.Provider, in.InstallationID}
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionConnectionManage, in.OrganizationID, versioncontrol.ActionConnectionCreate, proposal)
	if err != nil {
		return nil, err
	}
	return a.next.CompleteInstallation(ctx, actor, in)
}
