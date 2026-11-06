// Package projects implements workspace.ProjectService: the Projects in
// Workspaces, and their HTTP API. The domain types stay in workspace; this
// package orchestrates them.
//
// NewProjectService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller (the HTTP
// API, MCP, workers) is checked the same way. The service below assumes that
// check has happened and records the decision that allowed each change in
// its audit record.
package projects

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type ProjectServiceOption func(*options)

type options struct {
	repo       workspace.Repository
	resources  iam.ResourceRegistrar
	teams      team.Directory
	dependents []workspace.Dependent
	authz      authorization.Authorizer
	writer     change.Writer
}

func WithRepository(r workspace.Repository) ProjectServiceOption {
	return func(o *options) { o.repo = r }
}

// WithResourceRegistrar keeps IAM's resource hierarchy in step: a Project
// is a resource under its Workspace.
func WithResourceRegistrar(r iam.ResourceRegistrar) ProjectServiceOption {
	return func(o *options) { o.resources = r }
}

// WithTeams checks that a Project's owning Team is active in its Organization.
func WithTeams(d team.Directory) ProjectServiceOption {
	return func(o *options) { o.teams = d }
}

// WithDependents adds capabilities that keep records under Projects and
// must be asked before one is archived. It is optional and may be repeated.
func WithDependents(d ...workspace.Dependent) ProjectServiceOption {
	return func(o *options) { o.dependents = append(o.dependents, d...) }
}

func WithAuthorizer(a authorization.Authorizer) ProjectServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, its IAM resource, and its audit
// record commit together, so the repository, the outbox, the resource
// registrar, and the audit recorder must write through its context.
func WithTransactor(tx transaction.Transactor) ProjectServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) ProjectServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) ProjectServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewProjectService returns the ProjectService, authorizing every
// operation, or an error naming a missing dependency.
func NewProjectService(opts ...ProjectServiceOption) (workspace.ProjectService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Repository", o.repo != nil},
		{"ResourceRegistrar", o.resources != nil},
		{"Teams", o.teams != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("project service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("project service: %w", err)
	}
	s := &service{repo: o.repo, resources: o.resources, teams: o.teams, dependents: o.dependents, w: o.writer}
	return &authorized{next: s, authz: o.authz, repo: o.repo}, nil
}

// service carries out Project operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	repo       workspace.Repository
	resources  iam.ResourceRegistrar
	teams      team.Directory
	dependents []workspace.Dependent
	w          change.Writer
}

var _ workspace.ProjectService = (*service)(nil)

// CreateProject creates a Project in an active Workspace, owned by an active
// Team, and registers its IAM resource under the Workspace.
func (s *service) CreateProject(ctx context.Context, actor identity.Actor, in workspace.CreateProjectInput) (*workspace.Project, error) {
	p, err := workspace.NewProject(workspace.NewProjectProps{
		OrganizationID:       in.OrganizationID,
		WorkspaceID:          in.WorkspaceID,
		OwnerTeamID:          in.OwnerTeamID,
		Slug:                 in.Slug,
		DisplayName:          in.DisplayName,
		Description:          in.Description,
		CreatedByPrincipalID: actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := s.checkTeam(ctx, in.OrganizationID, in.OwnerTeamID); err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so the Workspace cannot be archived while the Project is created.
		w, err := s.repo.GetWorkspaceForUpdate(ctx, in.OrganizationID, in.WorkspaceID)
		if err != nil {
			return err
		}
		if w.Archived() {
			return fmt.Errorf("create project in workspace %s: %w", w.ID(), workspace.ErrArchived)
		}
		if err := s.repo.CreateProject(ctx, p); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := s.w.Events(ctx, workspace.AggregateProject, p.ID(), p); err != nil {
			return err
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: p.ID(), OrganizationID: p.OrganizationID(), Type: iam.ResourceProject, ParentID: p.WorkspaceID()}); err != nil {
			return fmt.Errorf("register project resource: %w", err)
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: workspace.EventProjectCreated, OrganizationID: p.OrganizationID(), ResourceID: p.ID(), Actor: actor, After: p.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *service) GetProject(ctx context.Context, _ identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	return s.repo.GetProject(ctx, organizationID, projectID)
}

func (s *service) ListProjects(ctx context.Context, _ identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*workspace.Project, error) {
	return s.repo.ListProjects(ctx, organizationID, workspaceID, includeArchived)
}

// change runs one change to a Project through change.Run. resource, if
// set, archives or restores the Project's IAM resource after it is written.
func (s *service) change(ctx context.Context, actor identity.Actor, organizationID, projectID, event string, apply func(context.Context, *workspace.Project) error, resource func(ctx context.Context, organizationID, resourceID string) error) (*workspace.Project, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*workspace.Project]{
		AggregateType: workspace.AggregateProject,
		AggregateID:   func(p *workspace.Project) string { return p.ID() },
		Load: func(ctx context.Context) (*workspace.Project, error) {
			return s.repo.GetProject(ctx, organizationID, projectID)
		},
		Apply: apply,
		Save: func(ctx context.Context, p *workspace.Project) error {
			if err := s.repo.UpdateProject(ctx, p); err != nil {
				return fmt.Errorf("update project: %w", err)
			}
			if resource != nil {
				if err := resource(ctx, organizationID, p.ID()); err != nil {
					return fmt.Errorf("project resource: %w", err)
				}
			}
			return nil
		},
		Snapshot: func(p *workspace.Project) any { return p.Snapshot() },
		Event: func(p *workspace.Project) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: p.ID()}
		},
	})
}

// UpdateProject renames or redescribes an active Project.
func (s *service) UpdateProject(ctx context.Context, actor identity.Actor, organizationID, projectID string, props workspace.UpdateProjectProps) (*workspace.Project, error) {
	return s.change(ctx, actor, organizationID, projectID, workspace.EventProjectUpdated,
		func(_ context.Context, p *workspace.Project) error { return p.Update(props) }, nil)
}

// TransferProject hands an active Project to another active Team.
func (s *service) TransferProject(ctx context.Context, actor identity.Actor, organizationID, projectID, ownerTeamID string) (*workspace.Project, error) {
	if err := s.checkTeam(ctx, organizationID, ownerTeamID); err != nil {
		return nil, err
	}
	return s.change(ctx, actor, organizationID, projectID, workspace.EventProjectOwnerChanged,
		func(_ context.Context, p *workspace.Project) error { return p.TransferTo(ownerTeamID) }, nil)
}

// ArchiveProject archives a Project with no active dependents. Archiving an
// archived Project does nothing.
func (s *service) ArchiveProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	return s.change(ctx, actor, organizationID, projectID, workspace.EventProjectArchived,
		func(ctx context.Context, p *workspace.Project) error {
			if p.Archived() {
				return change.ErrUnchanged
			}
			if err := workspace.CheckDependents(ctx, s.dependents, workspace.ArchiveTarget{OrganizationID: organizationID, WorkspaceID: p.WorkspaceID(), ProjectID: p.ID()}); err != nil {
				return err
			}
			p.Archive()
			return nil
		}, s.resources.ArchiveResource)
}

// RestoreProject restores an archived Project in an active Workspace, if its
// slug is still free. Restoring an active Project does nothing.
func (s *service) RestoreProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*workspace.Project, error) {
	return s.change(ctx, actor, organizationID, projectID, workspace.EventProjectRestored,
		func(ctx context.Context, p *workspace.Project) error {
			if !p.Archived() {
				return change.ErrUnchanged
			}
			// Locked so the Workspace cannot be archived while the Project is restored.
			w, err := s.repo.GetWorkspaceForUpdate(ctx, organizationID, p.WorkspaceID())
			if err != nil {
				return err
			}
			if w.Archived() {
				return fmt.Errorf("restore project into workspace %s: %w", w.ID(), workspace.ErrArchived)
			}
			p.Restore()
			return nil
		}, s.resources.RestoreResource)
}

func (s *service) checkTeam(ctx context.Context, organizationID, teamID string) error {
	if teamID == "" {
		return fmt.Errorf("%w: a project needs an owning team", workspace.ErrInvalid)
	}
	ok, err := s.teams.IsActiveTeam(ctx, organizationID, teamID)
	if err != nil {
		return fmt.Errorf("check team %s: %w", teamID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s", workspace.ErrInactiveTeam, teamID)
	}
	return nil
}
