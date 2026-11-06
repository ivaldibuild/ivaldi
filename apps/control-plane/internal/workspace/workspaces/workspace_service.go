// Package workspaces implements workspace.WorkspaceService: an
// Organization's Workspaces, and their HTTP API. The domain types stay in
// workspace; this package orchestrates them.
//
// NewWorkspaceService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller (the HTTP
// API, MCP, workers) is checked the same way. The service below assumes that
// check has happened and records the decision that allowed each change in
// its audit record.
package workspaces

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type WorkspaceServiceOption func(*options)

type options struct {
	repo       workspace.Repository
	resources  iam.ResourceRegistrar
	dependents []workspace.Dependent
	authz      authorization.Authorizer
	writer     change.Writer
}

func WithRepository(r workspace.Repository) WorkspaceServiceOption {
	return func(o *options) { o.repo = r }
}

// WithResourceRegistrar keeps IAM's resource hierarchy in step: a Workspace
// is a resource under its Organization.
func WithResourceRegistrar(r iam.ResourceRegistrar) WorkspaceServiceOption {
	return func(o *options) { o.resources = r }
}

// WithDependents adds capabilities that keep records under Workspaces and
// must be asked before one is archived. It is optional and may be repeated.
func WithDependents(d ...workspace.Dependent) WorkspaceServiceOption {
	return func(o *options) { o.dependents = append(o.dependents, d...) }
}

func WithAuthorizer(a authorization.Authorizer) WorkspaceServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, its IAM resource, and its audit
// record commit together, so the repository, the outbox, the resource
// registrar, and the audit recorder must write through its context.
func WithTransactor(tx transaction.Transactor) WorkspaceServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) WorkspaceServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) WorkspaceServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewWorkspaceService returns the WorkspaceService, authorizing every
// operation, or an error naming a missing dependency.
func NewWorkspaceService(opts ...WorkspaceServiceOption) (workspace.WorkspaceService, error) {
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
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("workspace service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("workspace service: %w", err)
	}
	s := &service{repo: o.repo, resources: o.resources, dependents: o.dependents, w: o.writer}
	return &authorized{next: s, authz: o.authz}, nil
}

// service carries out Workspace operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	repo       workspace.Repository
	resources  iam.ResourceRegistrar
	dependents []workspace.Dependent
	w          change.Writer
}

var _ workspace.WorkspaceService = (*service)(nil)

// CreateWorkspace creates a Workspace and registers its IAM resource under the Organization.
func (s *service) CreateWorkspace(ctx context.Context, actor identity.Actor, in workspace.CreateWorkspaceInput) (*workspace.Workspace, error) {
	w, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{
		OrganizationID:       in.OrganizationID,
		Slug:                 in.Slug,
		DisplayName:          in.DisplayName,
		Description:          in.Description,
		CreatedByPrincipalID: actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		if err := s.w.Events(ctx, workspace.AggregateWorkspace, w.ID(), w); err != nil {
			return err
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: w.ID(), OrganizationID: w.OrganizationID(), Type: iam.ResourceWorkspace, ParentID: w.OrganizationID()}); err != nil {
			return fmt.Errorf("register workspace resource: %w", err)
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: workspace.EventWorkspaceCreated, OrganizationID: w.OrganizationID(), ResourceID: w.ID(), Actor: actor, After: w.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *service) GetWorkspace(ctx context.Context, _ identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return s.repo.GetWorkspace(ctx, organizationID, workspaceID)
}

func (s *service) ListWorkspaces(ctx context.Context, _ identity.Actor, organizationID string, includeArchived bool) ([]*workspace.Workspace, error) {
	return s.repo.ListWorkspaces(ctx, organizationID, includeArchived)
}

// change runs one change to a Workspace through change.Run.
func (s *service) change(ctx context.Context, actor identity.Actor, organizationID, event string, load func(context.Context) (*workspace.Workspace, error), apply func(context.Context, *workspace.Workspace) error, save func(context.Context, *workspace.Workspace) error) (*workspace.Workspace, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*workspace.Workspace]{
		AggregateType: workspace.AggregateWorkspace,
		AggregateID:   func(w *workspace.Workspace) string { return w.ID() },
		Load:          load,
		Apply:         apply,
		Save:          save,
		Snapshot:      func(w *workspace.Workspace) any { return w.Snapshot() },
		Event: func(w *workspace.Workspace) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: w.ID()}
		},
	})
}

func (s *service) get(organizationID, workspaceID string) func(context.Context) (*workspace.Workspace, error) {
	return func(ctx context.Context) (*workspace.Workspace, error) {
		return s.repo.GetWorkspace(ctx, organizationID, workspaceID)
	}
}

func (s *service) save(ctx context.Context, w *workspace.Workspace) error {
	if err := s.repo.UpdateWorkspace(ctx, w); err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	return nil
}

// UpdateWorkspace renames or redescribes an active Workspace, including the default one.
func (s *service) UpdateWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, props workspace.UpdateWorkspaceProps) (*workspace.Workspace, error) {
	return s.change(ctx, actor, organizationID, workspace.EventWorkspaceUpdated, s.get(organizationID, workspaceID),
		func(_ context.Context, w *workspace.Workspace) error { return w.Update(props) },
		s.save)
}

// SetDefaultWorkspace makes an active Workspace the Organization's default,
// in place of the current one. Making the default the default again does
// nothing.
func (s *service) SetDefaultWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	var target *workspace.Workspace
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		target, err = s.repo.GetWorkspace(ctx, organizationID, workspaceID)
		if err != nil {
			return err
		}
		if target.IsDefault() {
			return nil
		}
		current, err := s.repo.GetDefaultWorkspace(ctx, organizationID)
		if err != nil && !errors.Is(err, workspace.ErrNotFound) {
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
			if err := s.w.Events(ctx, workspace.AggregateWorkspace, current.ID(), current); err != nil {
				return err
			}
		}
		if err := s.repo.UpdateWorkspace(ctx, target); err != nil {
			return fmt.Errorf("set default workspace: %w", err)
		}
		if err := s.w.Events(ctx, workspace.AggregateWorkspace, target.ID(), target); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: workspace.EventWorkspaceDefaultMoved, OrganizationID: organizationID, ResourceID: target.ID(), Actor: actor, Before: before, After: target.ID()})
	})
	if err != nil {
		return nil, err
	}
	return target, nil
}

// ArchiveWorkspace archives a Workspace that is not the default and has no
// active Projects or other dependents. Archiving an archived Workspace does nothing.
func (s *service) ArchiveWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return s.change(ctx, actor, organizationID, workspace.EventWorkspaceArchived,
		// Locked so a Project cannot be created between the check and the archive.
		func(ctx context.Context) (*workspace.Workspace, error) {
			return s.repo.GetWorkspaceForUpdate(ctx, organizationID, workspaceID)
		},
		func(ctx context.Context, w *workspace.Workspace) error {
			if w.Archived() {
				return change.ErrUnchanged
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
				return &workspace.DependentsError{Dependencies: []workspace.Dependency{{Capability: "workspace", Kind: "Project", Count: n}}}
			}
			if err := workspace.CheckDependents(ctx, s.dependents, workspace.ArchiveTarget{OrganizationID: organizationID, WorkspaceID: workspaceID}); err != nil {
				return err
			}
			return w.Archive()
		},
		func(ctx context.Context, w *workspace.Workspace) error {
			if err := s.save(ctx, w); err != nil {
				return err
			}
			if err := s.resources.ArchiveResource(ctx, organizationID, w.ID()); err != nil {
				return fmt.Errorf("archive workspace resource: %w", err)
			}
			return nil
		})
}

// RestoreWorkspace restores an archived Workspace if its slug is still free.
// Restoring an active Workspace does nothing.
func (s *service) RestoreWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return s.change(ctx, actor, organizationID, workspace.EventWorkspaceRestored, s.get(organizationID, workspaceID),
		func(_ context.Context, w *workspace.Workspace) error {
			if !w.Archived() {
				return change.ErrUnchanged
			}
			w.Restore()
			return nil
		},
		func(ctx context.Context, w *workspace.Workspace) error {
			if err := s.save(ctx, w); err != nil {
				return err
			}
			if err := s.resources.RestoreResource(ctx, organizationID, w.ID()); err != nil {
				return fmt.Errorf("restore workspace resource: %w", err)
			}
			return nil
		})
}
