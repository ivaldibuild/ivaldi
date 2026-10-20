package workspace

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CreateProjectInput describes a new Project.
type CreateProjectInput struct {
	OrganizationID string
	WorkspaceID    string
	OwnerTeamID    string
	Slug           string
	DisplayName    string
	Description    string
}

// CreateProject creates a Project in an active Workspace, owned by an active
// Team, and registers its IAM resource under the Workspace.
func (s *Service) CreateProject(ctx context.Context, actor identity.Actor, in CreateProjectInput) (*Project, error) {
	p, err := NewProject(NewProjectProps{
		OrganizationID:       in.OrganizationID,
		WorkspaceID:          in.WorkspaceID,
		OwnerTeamID:          in.OwnerTeamID,
		Slug:                 in.Slug,
		DisplayName:          in.DisplayName,
		Description:          in.Description,
		CreatedByPrincipalID: actor.PrincipalID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectManage, OrganizationID: in.OrganizationID,
		ResourceID: in.WorkspaceID, Action: ActionProjectCreate, Proposal: in,
	}); err != nil {
		return nil, err
	}
	if err := s.checkTeam(ctx, in.OrganizationID, in.OwnerTeamID); err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so the Workspace cannot be archived while the Project is created.
		w, err := s.repo.GetWorkspaceForUpdate(ctx, in.OrganizationID, in.WorkspaceID)
		if err != nil {
			return err
		}
		if w.Archived() {
			return fmt.Errorf("create project in workspace %s: %w", w.ID(), ErrArchived)
		}
		if err := s.repo.CreateProject(ctx, p); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: p.ID(), OrganizationID: p.OrganizationID(), Type: iam.ResourceProject, ParentID: p.WorkspaceID()}); err != nil {
			return fmt.Errorf("register project resource: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventProjectCreated, OrganizationID: p.OrganizationID(), ResourceID: p.ID(), Actor: actor, After: p.Data()})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject returns a Project the actor may read.
func (s *Service) GetProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*Project, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectRead, OrganizationID: organizationID,
		ResourceID: projectID, Action: ActionProjectRead,
	}); err != nil {
		return nil, err
	}
	return s.repo.GetProject(ctx, organizationID, projectID)
}

// ListProjects returns a Workspace's Projects. It needs read access on the
// Workspace; listing only the Projects each caller can see waits for IAM's
// resource listing.
func (s *Service) ListProjects(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*Project, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectRead, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: ActionProjectList,
	}); err != nil {
		return nil, err
	}
	return s.repo.ListProjects(ctx, organizationID, workspaceID, includeArchived)
}

// LookupProject returns a Project without authorizing. It is for other
// capabilities, such as delivery copying a Project's Workspace onto an Application.
func (s *Service) LookupProject(ctx context.Context, organizationID, projectID string) (*Project, error) {
	return s.repo.GetProject(ctx, organizationID, projectID)
}

// UpdateProject renames or redescribes an active Project.
func (s *Service) UpdateProject(ctx context.Context, actor identity.Actor, organizationID, projectID string, u UpdateProjectProps) (*Project, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectManage, OrganizationID: organizationID,
		ResourceID: projectID, Action: ActionProjectUpdate, Proposal: u,
	}); err != nil {
		return nil, err
	}
	return s.changeProject(ctx, actor, organizationID, projectID, EventProjectUpdated, func(p *Project) error {
		return p.Update(u)
	})
}

// TransferProject hands an active Project to another active Team. The
// Project's existing Applications keep their own owners.
func (s *Service) TransferProject(ctx context.Context, actor identity.Actor, organizationID, projectID, ownerTeamID string) (*Project, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectManage, OrganizationID: organizationID,
		ResourceID: projectID, Action: ActionProjectTransfer, Proposal: ownerTeamID,
	}); err != nil {
		return nil, err
	}
	if err := s.checkTeam(ctx, organizationID, ownerTeamID); err != nil {
		return nil, err
	}
	return s.changeProject(ctx, actor, organizationID, projectID, EventProjectOwnerChanged, func(p *Project) error {
		return p.TransferTo(ownerTeamID)
	})
}

// changeProject loads a Project, applies change through its domain methods,
// and persists it with an audit record.
func (s *Service) changeProject(ctx context.Context, actor identity.Actor, organizationID, projectID, event string, change func(*Project) error) (*Project, error) {
	var p *Project
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		p, err = s.repo.GetProject(ctx, organizationID, projectID)
		if err != nil {
			return err
		}
		before := p.Data()
		if err := change(p); err != nil {
			return err
		}
		if err := s.repo.UpdateProject(ctx, p); err != nil {
			return fmt.Errorf("update project: %w", err)
		}
		return s.record(ctx, audit.Event{Type: event, OrganizationID: organizationID, ResourceID: p.ID(), Actor: actor, Before: before, After: p.Data()})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ArchiveProject archives a Project with no active dependents. Archiving an
// archived Project does nothing.
func (s *Service) ArchiveProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*Project, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectManage, OrganizationID: organizationID,
		ResourceID: projectID, Action: ActionProjectArchive,
	}); err != nil {
		return nil, err
	}

	var p *Project
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error

		p, err = s.repo.GetProject(ctx, organizationID, projectID)
		if err != nil {
			return err
		}

		if p.Archived() {
			return nil
		}

		if err := s.checkDependents(ctx, ArchiveTarget{OrganizationID: organizationID, WorkspaceID: p.WorkspaceID(), ProjectID: p.ID()}); err != nil {
			return err
		}

		p.Archive()

		if err := s.repo.UpdateProject(ctx, p); err != nil {
			return fmt.Errorf("archive project: %w", err)
		}

		if err := s.resources.ArchiveResource(ctx, organizationID, p.ID()); err != nil {
			return fmt.Errorf("archive project resource: %w", err)
		}

		return s.record(ctx, audit.Event{Type: EventProjectArchived, OrganizationID: organizationID, ResourceID: p.ID(), Actor: actor})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// RestoreProject restores an archived Project in an active Workspace, if its
// slug is still free. It is authorized on the Workspace, because grants on an
// archived Project do not apply. Restoring an active Project does nothing.
func (s *Service) RestoreProject(ctx context.Context, actor identity.Actor, organizationID, workspaceID, projectID string) (*Project, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionProjectManage, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: ActionProjectRestore, Proposal: projectID,
	}); err != nil {
		return nil, err
	}

	var p *Project
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		p, err = s.repo.GetProject(ctx, organizationID, projectID)
		if err != nil {
			return err
		}

		// The authorization above was for this Workspace, so the Project must be in it.
		if p.WorkspaceID() != workspaceID {
			return fmt.Errorf("project %s in workspace %s: %w", projectID, workspaceID, ErrNotFound)
		}

		if !p.Archived() {
			return nil
		}

		w, err := s.repo.GetWorkspaceForUpdate(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}

		if w.Archived() {
			return fmt.Errorf("restore project into workspace %s: %w", workspaceID, ErrArchived)
		}

		p.Restore()

		if err := s.repo.UpdateProject(ctx, p); err != nil {
			return fmt.Errorf("restore project: %w", err)
		}

		if err := s.resources.RestoreResource(ctx, organizationID, p.ID()); err != nil {
			return fmt.Errorf("restore project resource: %w", err)
		}

		return s.record(ctx, audit.Event{Type: EventProjectRestored, OrganizationID: organizationID, ResourceID: p.ID(), Actor: actor})
	})

	if err != nil {
		return nil, err
	}

	return p, nil
}
