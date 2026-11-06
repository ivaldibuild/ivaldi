package workspaces

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// A Workspace is checked on itself, except where its own grants cannot
// apply: creating and listing are checked on the Organization, as are
// moving the default (which changes two Workspaces) and restoring (grants on
// an archived Workspace do not apply). Input that can never be valid is
// refused before asking, since checking it reads nothing.
type authorized struct {
	next  workspace.WorkspaceService
	authz authorization.Authorizer
}

var _ workspace.WorkspaceService = (*authorized)(nil)

// allow asks for a decision and returns ctx carrying it for the call.
func (a *authorized) allow(ctx context.Context, actor identity.Actor, permission, organizationID, resourceID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: resourceID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string) error {
	_, err := a.allow(ctx, actor, workspace.PermissionWorkspaceRead, organizationID, resourceID, action, nil)
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, actor, workspace.PermissionWorkspaceManage, organizationID, resourceID, action, proposal)
}

func (a *authorized) CreateWorkspace(ctx context.Context, actor identity.Actor, in workspace.CreateWorkspaceInput) (*workspace.Workspace, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.OrganizationID, workspace.ActionWorkspaceCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateWorkspace(ctx, actor, in)
}

func (a *authorized) GetWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	if err := a.read(ctx, actor, organizationID, workspaceID, workspace.ActionWorkspaceRead); err != nil {
		return nil, err
	}
	return a.next.GetWorkspace(ctx, actor, organizationID, workspaceID)
}

func (a *authorized) ListWorkspaces(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*workspace.Workspace, error) {
	if err := a.read(ctx, actor, organizationID, organizationID, workspace.ActionWorkspaceList); err != nil {
		return nil, err
	}
	return a.next.ListWorkspaces(ctx, actor, organizationID, includeArchived)
}

func (a *authorized) UpdateWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, props workspace.UpdateWorkspaceProps) (*workspace.Workspace, error) {
	if err := props.Validate(); err != nil {
		return nil, err
	}
	ctx, err := a.manage(ctx, actor, organizationID, workspaceID, workspace.ActionWorkspaceUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateWorkspace(ctx, actor, organizationID, workspaceID, props)
}

func (a *authorized) SetDefaultWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	ctx, err := a.manage(ctx, actor, organizationID, organizationID, workspace.ActionWorkspaceSetDefault, workspaceID)
	if err != nil {
		return nil, err
	}
	return a.next.SetDefaultWorkspace(ctx, actor, organizationID, workspaceID)
}

func (a *authorized) ArchiveWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	ctx, err := a.manage(ctx, actor, organizationID, workspaceID, workspace.ActionWorkspaceArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveWorkspace(ctx, actor, organizationID, workspaceID)
}

func (a *authorized) RestoreWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	ctx, err := a.manage(ctx, actor, organizationID, organizationID, workspace.ActionWorkspaceRestore, workspaceID)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreWorkspace(ctx, actor, organizationID, workspaceID)
}
