package projects

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
// A Project is checked on itself, except where its own grants cannot apply:
// creating and listing are checked on the Workspace, and so is restoring,
// because grants on an archived Project do not apply. Finding the Workspace
// to restore into means reading the Project first; a Project never moves
// between Workspaces, so the Workspace read here is the one next restores
// it in. Input that can never be valid is refused before asking, since
// checking it reads nothing.
type authorized struct {
	next  workspace.ProjectService
	authz authorization.Authorizer
	repo  workspace.Repository
}

var _ workspace.ProjectService = (*authorized)(nil)

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
	_, err := a.allow(ctx, actor, workspace.PermissionProjectRead, organizationID, resourceID, action, nil)
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, actor, workspace.PermissionProjectManage, organizationID, resourceID, action, proposal)
}

func (a *authorized) CreateProject(ctx context.Context, actor identity.Actor, in workspace.CreateProjectInput) (*workspace.Project, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.WorkspaceID, workspace.ActionProjectCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateProject(ctx, actor, in)
}

func (a *authorized) GetProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	if err := a.read(ctx, actor, organizationID, projectID, workspace.ActionProjectRead); err != nil {
		return nil, err
	}
	return a.next.GetProject(ctx, actor, organizationID, projectID)
}

func (a *authorized) ListProjects(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*workspace.Project, error) {
	if err := a.read(ctx, actor, organizationID, workspaceID, workspace.ActionProjectList); err != nil {
		return nil, err
	}
	return a.next.ListProjects(ctx, actor, organizationID, workspaceID, includeArchived)
}

func (a *authorized) UpdateProject(ctx context.Context, actor identity.Actor, organizationID, projectID string, props workspace.UpdateProjectProps) (*workspace.Project, error) {
	if err := props.Validate(); err != nil {
		return nil, err
	}
	ctx, err := a.manage(ctx, actor, organizationID, projectID, workspace.ActionProjectUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateProject(ctx, actor, organizationID, projectID, props)
}

func (a *authorized) TransferProject(ctx context.Context, actor identity.Actor, organizationID, projectID, ownerTeamID string) (*workspace.Project, error) {
	ctx, err := a.manage(ctx, actor, organizationID, projectID, workspace.ActionProjectTransfer, ownerTeamID)
	if err != nil {
		return nil, err
	}
	return a.next.TransferProject(ctx, actor, organizationID, projectID, ownerTeamID)
}

func (a *authorized) ArchiveProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	ctx, err := a.manage(ctx, actor, organizationID, projectID, workspace.ActionProjectArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveProject(ctx, actor, organizationID, projectID)
}

// RestoreProject finds the Project before authorizing, to check it on its
// Workspace. Someone without access learns only whether the Project exists.
func (a *authorized) RestoreProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	p, err := a.repo.GetProject(ctx, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	ctx, err = a.manage(ctx, actor, organizationID, p.WorkspaceID(), workspace.ActionProjectRestore, projectID)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreProject(ctx, actor, organizationID, projectID)
}
