// Package environments implements delivery.EnvironmentService: Environments,
// the Targets in them, and their HTTP API. The domain types stay in
// delivery; this package orchestrates them.
//
// NewEnvironmentService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller (the HTTP
// API, MCP, workers) is checked the same way. The service below assumes that
// check has happened and records the decision that allowed each change in
// its audit record.
package environments

import (
	"context"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// LifecycleUsage counts the Lifecycles an Environment is in, which keep it
// from being archived. delivery.LifecycleRepository provides it.
type LifecycleUsage interface {
	CountActiveLifecyclesWithEnvironment(ctx context.Context, organizationID, environmentID string) (int, error)
}

type EnvironmentServiceOption func(*options)

type options struct {
	environments delivery.EnvironmentRepository
	targets      delivery.TargetRepository
	lifecycles   LifecycleUsage
	workspaces   delivery.Workspaces
	resources    iam.ResourceRegistrar
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithEnvironmentRepo(r delivery.EnvironmentRepository) EnvironmentServiceOption {
	return func(o *options) { o.environments = r }
}
func WithTargetRepo(r delivery.TargetRepository) EnvironmentServiceOption {
	return func(o *options) { o.targets = r }
}
func WithLifecycleUsage(l LifecycleUsage) EnvironmentServiceOption {
	return func(o *options) { o.lifecycles = l }
}
func WithWorkspaces(w delivery.Workspaces) EnvironmentServiceOption {
	return func(o *options) { o.workspaces = w }
}

// WithResourceRegistrar keeps IAM's resource hierarchy in step: an
// Environment is a resource under its Workspace.
func WithResourceRegistrar(r iam.ResourceRegistrar) EnvironmentServiceOption {
	return func(o *options) { o.resources = r }
}
func WithAuthorizer(a authorization.Authorizer) EnvironmentServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, its IAM resource, and its audit
// record commit together, so the repositories, the outbox, the resource
// registrar, and the audit recorder must write through its context.
func WithTransactor(tx transaction.Transactor) EnvironmentServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) EnvironmentServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) EnvironmentServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewEnvironmentService returns the EnvironmentService, authorizing every
// operation, or an error naming a missing dependency.
func NewEnvironmentService(opts ...EnvironmentServiceOption) (delivery.EnvironmentService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"EnvironmentRepo", o.environments != nil},
		{"TargetRepo", o.targets != nil},
		{"LifecycleUsage", o.lifecycles != nil},
		{"Workspaces", o.workspaces != nil},
		{"ResourceRegistrar", o.resources != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("environment service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("environment service: %w", err)
	}
	s := &service{
		environments: o.environments,
		targets:      o.targets,
		lifecycles:   o.lifecycles,
		workspaces:   o.workspaces,
		resources:    o.resources,
		w:            o.writer,
	}
	return &authorized{next: s, authz: o.authz, environments: o.environments, targets: o.targets}, nil
}

// service carries out Environment and Target operations once they are
// authorized. It checks rules that span records, changes objects only
// through their methods, and commits each change with its events and audit
// record.
type service struct {
	environments delivery.EnvironmentRepository
	targets      delivery.TargetRepository
	lifecycles   LifecycleUsage
	workspaces   delivery.Workspaces
	resources    iam.ResourceRegistrar
	w            change.Writer
}

var _ delivery.EnvironmentService = (*service)(nil)

// CreateEnvironment creates an Environment in an active Workspace and
// registers its IAM resource under the Workspace.
func (s *service) CreateEnvironment(ctx context.Context, actor identity.Actor, in delivery.CreateEnvironmentInput) (*delivery.Environment, error) {
	e, err := delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID:   in.OrganizationID,
		WorkspaceID:      in.WorkspaceID,
		Slug:             in.Slug,
		DisplayName:      in.DisplayName,
		Tier:             in.Tier,
		ExecutionEnabled: in.ExecutionEnabled,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := delivery.ActiveWorkspace(ctx, s.workspaces, e.OrganizationID(), e.WorkspaceID()); err != nil {
			return err
		}
		if err := s.environments.CreateEnvironment(ctx, e); err != nil {
			return fmt.Errorf("create environment: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateEnvironment, e.ID(), e); err != nil {
			return err
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: e.ID(), OrganizationID: e.OrganizationID(), Type: iam.ResourceEnvironment, ParentID: e.WorkspaceID()}); err != nil {
			return fmt.Errorf("register environment resource: %w", err)
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventEnvironmentCreated, OrganizationID: e.OrganizationID(), ResourceID: e.ID(), Actor: actor, After: e.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *service) GetEnvironment(ctx context.Context, _ identity.Actor, organizationID, environmentID string) (*delivery.Environment, error) {
	return s.environments.GetEnvironment(ctx, organizationID, environmentID)
}

func (s *service) GetEnvironmentBySlug(ctx context.Context, _ identity.Actor, organizationID, workspaceID, slug string) (*delivery.Environment, error) {
	return s.environments.GetEnvironmentBySlug(ctx, organizationID, workspaceID, slug)
}

func (s *service) ListEnvironments(ctx context.Context, _ identity.Actor, query delivery.ListEnvironmentsQuery) (*pagination.Result[*delivery.Environment], error) {
	return s.environments.ListEnvironments(ctx, query)
}

// UpdateEnvironment changes an active Environment.
func (s *service) UpdateEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string, props delivery.UpdateEnvironmentProps) (*delivery.Environment, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Environment]{
		AggregateType: delivery.AggregateEnvironment,
		AggregateID:   func(e *delivery.Environment) string { return e.ID() },
		Load: func(ctx context.Context) (*delivery.Environment, error) {
			return s.environments.GetEnvironment(ctx, organizationID, environmentID)
		},
		Apply:    func(_ context.Context, e *delivery.Environment) error { return e.Update(props) },
		Save:     s.environments.UpdateEnvironment,
		Snapshot: func(e *delivery.Environment) any { return e.Snapshot() },
		Event: func(e *delivery.Environment) audit.Event {
			return audit.Event{Type: delivery.EventEnvironmentUpdated, OrganizationID: organizationID, ResourceID: e.ID()}
		},
	})
}

// ArchiveEnvironments archives Environments that have no active Targets and
// are in no active Lifecycle, all together or none of them. Archived ones
// are left as they are.
func (s *service) ArchiveEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*delivery.Environment, error) {
	ids := uniqueIDs(environmentIDs)
	var out []*delivery.Environment
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		out = out[:0]
		for _, id := range ids {
			// Locked so a Target cannot be added between the check and the archive.
			e, err := s.environments.GetEnvironmentForUpdate(ctx, organizationID, id)
			if err != nil {
				return err
			}
			out = append(out, e)
			if e.Archived() {
				continue
			}
			if err := s.checkUnused(ctx, e); err != nil {
				return err
			}
			before := e.Snapshot()
			e.Archive()
			if err := s.environments.UpdateEnvironment(ctx, e); err != nil {
				return fmt.Errorf("archive environment %s: %w", id, err)
			}
			if err := s.w.Events(ctx, delivery.AggregateEnvironment, e.ID(), e); err != nil {
				return err
			}
			if err := s.resources.ArchiveResource(ctx, organizationID, id); err != nil {
				return fmt.Errorf("archive environment resource %s: %w", id, err)
			}
			if err := s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventEnvironmentArchived, OrganizationID: organizationID, ResourceID: id, Actor: actor, Before: before, After: e.Snapshot()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) checkUnused(ctx context.Context, e *delivery.Environment) error {
	n, err := s.targets.CountActiveTargets(ctx, e.OrganizationID(), e.ID())
	if err != nil {
		return fmt.Errorf("count targets: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: environment %s has %d active targets", delivery.ErrInUse, e.ID(), n)
	}
	n, err = s.lifecycles.CountActiveLifecyclesWithEnvironment(ctx, e.OrganizationID(), e.ID())
	if err != nil {
		return fmt.Errorf("count lifecycles: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: environment %s is in %d active lifecycles", delivery.ErrInUse, e.ID(), n)
	}
	return nil
}

// RestoreEnvironments restores archived Environments in active Workspaces,
// all together or none of them.
func (s *service) RestoreEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*delivery.Environment, error) {
	ids := uniqueIDs(environmentIDs)
	var out []*delivery.Environment
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		out = out[:0]
		for _, id := range ids {
			e, err := s.environments.GetEnvironmentForUpdate(ctx, organizationID, id)
			if err != nil {
				return err
			}
			out = append(out, e)
			if !e.Archived() {
				continue
			}
			if err := delivery.ActiveWorkspace(ctx, s.workspaces, organizationID, e.WorkspaceID()); err != nil {
				return err
			}
			before := e.Snapshot()
			e.Restore()
			if err := s.environments.UpdateEnvironment(ctx, e); err != nil {
				return fmt.Errorf("restore environment %s: %w", id, err)
			}
			if err := s.w.Events(ctx, delivery.AggregateEnvironment, e.ID(), e); err != nil {
				return err
			}
			if err := s.resources.RestoreResource(ctx, organizationID, id); err != nil {
				return fmt.Errorf("restore environment resource %s: %w", id, err)
			}
			if err := s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventEnvironmentRestored, OrganizationID: organizationID, ResourceID: id, Actor: actor, Before: before, After: e.Snapshot()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateTarget adds a Target to an active Environment.
func (s *service) CreateTarget(ctx context.Context, actor identity.Actor, in delivery.CreateTargetInput) (*delivery.Target, error) {
	t, err := delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID:   in.OrganizationID,
		EnvironmentID:    in.EnvironmentID,
		Slug:             in.Slug,
		DisplayName:      in.DisplayName,
		Runtime:          in.Runtime,
		Locator:          in.Locator,
		AgentPrincipalID: &in.AgentPrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so the Environment cannot be archived while the Target is added.
		e, err := s.environments.GetEnvironmentForUpdate(ctx, in.OrganizationID, in.EnvironmentID)
		if err != nil {
			return err
		}
		if e.Archived() {
			return fmt.Errorf("%w: environment %s", delivery.ErrArchived, e.ID())
		}
		if err := s.targets.CreateTarget(ctx, t); err != nil {
			return fmt.Errorf("create target: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateTarget, t.ID(), t); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventTargetCreated, OrganizationID: t.OrganizationID(), ResourceID: t.ID(), Actor: actor, After: t.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *service) GetTarget(ctx context.Context, _ identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	return s.targets.GetTarget(ctx, organizationID, targetID)
}

func (s *service) ListTargets(ctx context.Context, _ identity.Actor, query delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	return s.targets.ListTargets(ctx, query)
}

func (s *service) changeTarget(ctx context.Context, actor identity.Actor, organizationID, targetID, event string, apply func(context.Context, *delivery.Target) error) (*delivery.Target, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Target]{
		AggregateType: delivery.AggregateTarget,
		AggregateID:   func(t *delivery.Target) string { return t.ID() },
		Load: func(ctx context.Context) (*delivery.Target, error) {
			return s.targets.GetTarget(ctx, organizationID, targetID)
		},
		Apply:    apply,
		Save:     s.targets.UpdateTarget,
		Snapshot: func(t *delivery.Target) any { return t.Snapshot() },
		Event: func(t *delivery.Target) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: t.ID()}
		},
	})
}

// UpdateTarget changes an active Target.
func (s *service) UpdateTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string, props delivery.UpdateTargetProps) (*delivery.Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, delivery.EventTargetUpdated,
		func(_ context.Context, t *delivery.Target) error { return t.Update(props) })
}

// AssignTargetAgent makes principalID the Target's runtime agent, or
// unassigns the agent when principalID is empty.
func (s *service) AssignTargetAgent(ctx context.Context, actor identity.Actor, organizationID, targetID, principalID string) (*delivery.Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, delivery.EventTargetAgentAssigned,
		func(_ context.Context, t *delivery.Target) error {
			if principalID == "" {
				if t.AgentPrincipalID() == nil && !t.Archived() {
					return change.ErrUnchanged
				}
				return t.UnassignAgent()
			}
			return t.AssignAgent(principalID)
		})
}

// RecordTargetReport notes that the Target's agent is in contact. The domain
// refuses anyone but the assigned agent. It records no audit event: reports
// are frequent, and change no configuration.
func (s *service) RecordTargetReport(ctx context.Context, actor identity.Actor, organizationID, targetID string) error {
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		t, err := s.targets.GetTarget(ctx, organizationID, targetID)
		if err != nil {
			return err
		}
		if err := t.RecordReport(actor.PrincipalID); err != nil {
			return err
		}
		return s.targets.UpdateTarget(ctx, t)
	})
}

// ArchiveTarget archives a Target. Archiving an archived one does nothing.
func (s *service) ArchiveTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, delivery.EventTargetArchived,
		func(_ context.Context, t *delivery.Target) error {
			if t.Archived() {
				return change.ErrUnchanged
			}
			t.Archive()
			return nil
		})
}

// RestoreTarget restores an archived Target in an active Environment.
func (s *service) RestoreTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, delivery.EventTargetRestored,
		func(ctx context.Context, t *delivery.Target) error {
			if !t.Archived() {
				return change.ErrUnchanged
			}
			e, err := s.environments.GetEnvironmentForUpdate(ctx, organizationID, t.EnvironmentID())
			if err != nil {
				return err
			}
			if e.Archived() {
				return fmt.Errorf("%w: environment %s", delivery.ErrArchived, e.ID())
			}
			t.Restore()
			return nil
		})
}

// uniqueIDs returns ids without repeats, in their first order.
func uniqueIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
