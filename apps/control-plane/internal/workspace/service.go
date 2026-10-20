package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Service orchestrates the workspace capability: it authorizes, checks rules
// that span several objects or capabilities, calls domain methods to change
// state, and persists the result with its audit record. It never sets a
// domain object's state itself. Actor-facing methods authorize through the
// single authorizer; Lookup methods are for other capabilities and do not.
type Service struct {
	repo        Repository
	tx          transaction.Transactor
	authz       authorization.Authorizer
	resources   iam.ResourceRegistrar
	teams       team.Directory
	audit       audit.Recorder
	locks       LockSource
	definitions *Definitions
	dependents  []Dependent
}

// Config holds the Service's collaborators. Everything except Dependents
// is required.
type Config struct {
	Repository Repository
	// Transactor makes a change, its IAM resource, and its audit record commit
	// together, so Resources and Audit must write through the context it passes.
	Transactor  transaction.Transactor
	Authorizer  authorization.Authorizer
	Resources   iam.ResourceRegistrar
	Teams       team.Directory
	Audit       audit.Recorder
	Locks       LockSource
	Definitions *Definitions
	// Dependents are the capabilities that keep records under Workspaces or
	// Projects and must be asked before either is archived.
	Dependents []Dependent
}

// NewService returns a Service, or an error naming a missing collaborator.
func NewService(c Config) (*Service, error) {
	required := map[string]bool{
		"Repository":  c.Repository != nil,
		"Transactor":  c.Transactor != nil,
		"Authorizer":  c.Authorizer != nil,
		"Resources":   c.Resources != nil,
		"Teams":       c.Teams != nil,
		"Audit":       c.Audit != nil,
		"Locks":       c.Locks != nil,
		"Definitions": c.Definitions != nil,
	}
	for name, ok := range required {
		if !ok {
			return nil, fmt.Errorf("workspace service: %s is required", name)
		}
	}
	s := &Service{
		repo:        c.Repository,
		tx:          c.Transactor,
		authz:       c.Authorizer,
		resources:   c.Resources,
		teams:       c.Teams,
		audit:       c.Audit,
		locks:       c.Locks,
		definitions: c.Definitions,
		dependents:  c.Dependents,
	}
	return s, nil
}

// authorize asks the single authorizer and turns anything but an allow into an error.
func (s *Service) authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	return authorization.Require(ctx, s.authz, req)
}

func (s *Service) record(ctx context.Context, e audit.Event) error {
	e.OccurredAt = now()
	if err := s.audit.Record(ctx, e); err != nil {
		return fmt.Errorf("record %s: %w", e.Type, err)
	}
	return nil
}

func (s *Service) checkTeam(ctx context.Context, organizationID, teamID string) error {
	if teamID == "" {
		return fmt.Errorf("%w: a project needs an owning team", ErrInvalid)
	}
	ok, err := s.teams.IsActiveTeam(ctx, organizationID, teamID)
	if err != nil {
		return fmt.Errorf("check team %s: %w", teamID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrInactiveTeam, teamID)
	}
	return nil
}

func (s *Service) checkDependents(ctx context.Context, target ArchiveTarget) error {
	var active []Dependency
	for _, d := range s.dependents {
		deps, err := d.ActiveDependents(ctx, target)
		if err != nil {
			return fmt.Errorf("check dependents: %w", err)
		}
		for _, dep := range deps {
			if dep.Count > 0 {
				active = append(active, dep)
			}
		}
	}
	if len(active) > 0 {
		return &DependentsError{Dependencies: active}
	}
	return nil
}

// ProvisionOrganization creates a new Organization's default Workspace. IAM
// calls it while creating the Organization, inside that transaction, so an
// Organization never exists without its default Workspace.
func (s *Service) ProvisionOrganization(ctx context.Context, organizationID string) (*Workspace, error) {
	w, err := NewDefaultWorkspace(organizationID)
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("create default workspace: %w", err)
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: w.ID(), OrganizationID: organizationID, Type: iam.ResourceWorkspace, ParentID: organizationID}); err != nil {
			return fmt.Errorf("register workspace resource: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceCreated, OrganizationID: organizationID, ResourceID: w.ID(), Actor: identity.Actor{Kind: identity.ActorSystem}, After: w.Data()})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// CreateWorkspaceInput describes a new Workspace.
type CreateWorkspaceInput struct {
	OrganizationID string
	Slug           string
	DisplayName    string
	Description    string
}

// CreateWorkspace creates a Workspace and registers its IAM resource under the Organization.
func (s *Service) CreateWorkspace(ctx context.Context, actor identity.Actor, in CreateWorkspaceInput) (*Workspace, error) {
	w, err := NewWorkspace(NewWorkspaceProps{
		OrganizationID:       in.OrganizationID,
		Slug:                 in.Slug,
		DisplayName:          in.DisplayName,
		Description:          in.Description,
		CreatedByPrincipalID: actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceManage, OrganizationID: in.OrganizationID,
		ResourceID: in.OrganizationID, Action: ActionWorkspaceCreate, Proposal: in,
	}); err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: w.ID(), OrganizationID: w.OrganizationID(), Type: iam.ResourceWorkspace, ParentID: w.OrganizationID()}); err != nil {
			return fmt.Errorf("register workspace resource: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceCreated, OrganizationID: w.OrganizationID(), ResourceID: w.ID(), Actor: actor, After: w.Data()})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// GetWorkspace returns a Workspace the actor may read.
func (s *Service) GetWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceRead, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: ActionWorkspaceRead,
	}); err != nil {
		return nil, err
	}
	return s.repo.GetWorkspace(ctx, organizationID, workspaceID)
}

// ListWorkspaces returns the Organization's Workspaces. It needs read access
// at Organization level; listing only the Workspaces each caller can see
// waits for IAM's resource listing.
func (s *Service) ListWorkspaces(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*Workspace, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: ActionWorkspaceList,
	}); err != nil {
		return nil, err
	}
	return s.repo.ListWorkspaces(ctx, organizationID, includeArchived)
}

// LookupWorkspace returns a Workspace without authorizing. It is for other
// capabilities, such as delivery checking a Workspace boundary.
func (s *Service) LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*Workspace, error) {
	return s.repo.GetWorkspace(ctx, organizationID, workspaceID)
}

// UpdateWorkspace renames or redescribes an active Workspace, including the default one.
func (s *Service) UpdateWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, u UpdateWorkspaceProps) (*Workspace, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceManage, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: ActionWorkspaceUpdate, Proposal: u,
	}); err != nil {
		return nil, err
	}

	var w *Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		w, err = s.repo.GetWorkspace(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}
		before := w.Data()
		if err := w.Update(u); err != nil {
			return err
		}
		if err := s.repo.UpdateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("update workspace: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceUpdated, OrganizationID: organizationID, ResourceID: w.ID(), Actor: actor, Before: before, After: w.Data()})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// SetDefaultWorkspace makes an active Workspace the Organization's default,
// in place of the current one.
func (s *Service) SetDefaultWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceManage, OrganizationID: organizationID,
		ResourceID: organizationID, Action: ActionWorkspaceSetDefault, Proposal: workspaceID,
	}); err != nil {
		return nil, err
	}

	var target *Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		target, err = s.repo.GetWorkspace(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}
		if target.IsDefault() {
			return nil
		}
		current, err := s.repo.GetDefaultWorkspace(ctx, organizationID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("get default workspace: %w", err)
		}
		if err := target.MakeDefault(); err != nil {
			return err
		}
		// Clear the old default first: at most one default is allowed at a time.
		var before string
		if current != nil {
			before = current.ID()
			current.ClearDefault()
			if err := s.repo.UpdateWorkspace(ctx, current); err != nil {
				return fmt.Errorf("clear default workspace: %w", err)
			}
		}
		if err := s.repo.UpdateWorkspace(ctx, target); err != nil {
			return fmt.Errorf("set default workspace: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceDefaultMoved, OrganizationID: organizationID, ResourceID: target.ID(), Actor: actor, Before: before, After: target.ID()})
	})
	if err != nil {
		return nil, err
	}
	return target, nil
}

// ArchiveWorkspace archives a Workspace that is not the default and has no
// active Projects or other dependents. Archiving an archived Workspace does nothing.
func (s *Service) ArchiveWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceManage, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: ActionWorkspaceArchive,
	}); err != nil {
		return nil, err
	}

	var w *Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		// Locked so a Project cannot be created between the check and the archive.
		w, err = s.repo.GetWorkspaceForUpdate(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}
		if w.Archived() {
			return nil
		}
		if w.IsDefault() {
			// Checked before the dependents, so the clearer refusal comes first.
			return w.Archive()
		}
		n, err := s.repo.CountActiveProjects(ctx, organizationID, workspaceID)
		if err != nil {
			return fmt.Errorf("count projects: %w", err)
		}
		if n > 0 {
			return &DependentsError{Dependencies: []Dependency{{Capability: "workspace", Kind: "Project", Count: n}}}
		}
		if err := s.checkDependents(ctx, ArchiveTarget{OrganizationID: organizationID, WorkspaceID: workspaceID}); err != nil {
			return err
		}
		if err := w.Archive(); err != nil {
			return err
		}
		if err := s.repo.UpdateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("archive workspace: %w", err)
		}
		if err := s.resources.ArchiveResource(ctx, organizationID, w.ID()); err != nil {
			return fmt.Errorf("archive workspace resource: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceArchived, OrganizationID: organizationID, ResourceID: w.ID(), Actor: actor})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// RestoreWorkspace restores an archived Workspace if its slug is still free.
// It is authorized at Organization level, because grants on an archived
// Workspace do not apply. Restoring an active Workspace does nothing.
func (s *Service) RestoreWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionWorkspaceManage, OrganizationID: organizationID,
		ResourceID: organizationID, Action: ActionWorkspaceRestore, Proposal: workspaceID,
	}); err != nil {
		return nil, err
	}

	var w *Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		w, err = s.repo.GetWorkspace(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}
		if !w.Archived() {
			return nil
		}
		w.Restore()
		if err := s.repo.UpdateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("restore workspace: %w", err)
		}
		if err := s.resources.RestoreResource(ctx, organizationID, w.ID()); err != nil {
			return fmt.Errorf("restore workspace resource: %w", err)
		}
		return s.record(ctx, audit.Event{Type: EventWorkspaceRestored, OrganizationID: organizationID, ResourceID: w.ID(), Actor: actor})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}
