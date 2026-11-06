// Package lifecycles implements delivery.LifecycleService: Lifecycles, the
// phases a Release moves through, and their HTTP API. The domain types stay
// in delivery; this package orchestrates them.
//
// NewLifecycleService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and
// records the decision that allowed each change in its audit record.
package lifecycles

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// EnvironmentLookup reads the Environments a Lifecycle's phases name, to
// check they are active and in its Workspace.
// delivery.EnvironmentRepository provides it.
type EnvironmentLookup interface {
	GetEnvironments(ctx context.Context, organizationID string, environmentIDs []string) ([]*delivery.Environment, error)
}

// ChannelUsage counts the active Channels of active Applications that follow
// a Lifecycle, which keep it from being archived.
// delivery.ApplicationRepository provides it.
type ChannelUsage interface {
	CountActiveChannelsWithLifecycle(ctx context.Context, organizationID, lifecycleID string) (int, error)
}

type LifecycleServiceOption func(*options)

type options struct {
	lifecycles   delivery.LifecycleRepository
	environments EnvironmentLookup
	channels     ChannelUsage
	workspaces   delivery.Workspaces
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithLifecycleRepo(r delivery.LifecycleRepository) LifecycleServiceOption {
	return func(o *options) { o.lifecycles = r }
}
func WithEnvironmentLookup(e EnvironmentLookup) LifecycleServiceOption {
	return func(o *options) { o.environments = e }
}
func WithChannelUsage(c ChannelUsage) LifecycleServiceOption {
	return func(o *options) { o.channels = c }
}
func WithWorkspaces(w delivery.Workspaces) LifecycleServiceOption {
	return func(o *options) { o.workspaces = w }
}
func WithAuthorizer(a authorization.Authorizer) LifecycleServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repositories, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) LifecycleServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) LifecycleServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) LifecycleServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewLifecycleService returns the LifecycleService, authorizing every
// operation, or an error naming a missing dependency.
func NewLifecycleService(opts ...LifecycleServiceOption) (delivery.LifecycleService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"LifecycleRepo", o.lifecycles != nil},
		{"EnvironmentLookup", o.environments != nil},
		{"ChannelUsage", o.channels != nil},
		{"Workspaces", o.workspaces != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("lifecycle service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("lifecycle service: %w", err)
	}
	s := &service{
		lifecycles:   o.lifecycles,
		environments: o.environments,
		channels:     o.channels,
		workspaces:   o.workspaces,
		w:            o.writer,
	}
	return &authorized{next: s, authz: o.authz, lifecycles: o.lifecycles}, nil
}

// service carries out Lifecycle operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	lifecycles   delivery.LifecycleRepository
	environments EnvironmentLookup
	channels     ChannelUsage
	workspaces   delivery.Workspaces
	w            change.Writer
}

var _ delivery.LifecycleService = (*service)(nil)

// CreateLifecycle creates a Lifecycle in an active Workspace. Its phases may
// name only that Workspace's active Environments.
func (s *service) CreateLifecycle(ctx context.Context, actor identity.Actor, in delivery.CreateLifecycleInput) (*delivery.Lifecycle, error) {
	phases := make([]delivery.NewLifecyclePhaseProps, len(in.Phases))
	for i, p := range in.Phases {
		phases[i] = delivery.NewLifecyclePhaseProps{Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs}
	}
	l, err := delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: in.OrganizationID,
		WorkspaceID:    in.WorkspaceID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Description:    &in.Description,
		Phases:         phases,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := delivery.ActiveWorkspace(ctx, s.workspaces, l.OrganizationID(), l.WorkspaceID()); err != nil {
			return err
		}
		if err := s.checkEnvironments(ctx, l); err != nil {
			return err
		}
		if err := s.lifecycles.CreateLifecycle(ctx, l); err != nil {
			return fmt.Errorf("create lifecycle: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateLifecycle, l.ID(), l); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventLifecycleCreated, OrganizationID: l.OrganizationID(), ResourceID: l.ID(), Actor: actor, After: l.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// checkEnvironments checks that every Environment in the Lifecycle's phases
// exists, is active, and is in the Lifecycle's Workspace.
func (s *service) checkEnvironments(ctx context.Context, l *delivery.Lifecycle) error {
	ids := l.EnvironmentIDs()
	if len(ids) == 0 {
		return nil
	}
	envs, err := s.environments.GetEnvironments(ctx, l.OrganizationID(), ids)
	if err != nil {
		return fmt.Errorf("get environments: %w", err)
	}
	byID := make(map[string]*delivery.Environment, len(envs))
	for _, e := range envs {
		byID[e.ID()] = e
	}
	for _, id := range ids {
		e, ok := byID[id]
		switch {
		case !ok:
			return fmt.Errorf("%w: environment %s", delivery.ErrNotFound, id)
		case e.WorkspaceID() != l.WorkspaceID():
			return fmt.Errorf("%w: environment %s is not in workspace %s", delivery.ErrWorkspaceMismatch, id, l.WorkspaceID())
		case e.Archived():
			return fmt.Errorf("%w: environment %s", delivery.ErrArchived, id)
		}
	}
	return nil
}

func (s *service) GetLifecycle(ctx context.Context, _ identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	return s.lifecycles.GetLifecycle(ctx, organizationID, lifecycleID)
}

func (s *service) ListLifecycles(ctx context.Context, _ identity.Actor, query delivery.ListLifecyclesQuery) (*pagination.Result[*delivery.Lifecycle], error) {
	return s.lifecycles.ListLifecycles(ctx, query)
}

func (s *service) changeLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, event string, apply func(context.Context, *delivery.Lifecycle) error) (*delivery.Lifecycle, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Lifecycle]{
		AggregateType: delivery.AggregateLifecycle,
		AggregateID:   func(l *delivery.Lifecycle) string { return l.ID() },
		// Locked so a Channel cannot start following it while it is archived.
		Load: func(ctx context.Context) (*delivery.Lifecycle, error) {
			return s.lifecycles.GetLifecycleForUpdate(ctx, organizationID, lifecycleID)
		},
		Apply:    apply,
		Save:     s.lifecycles.UpdateLifecycle,
		Snapshot: func(l *delivery.Lifecycle) any { return l.Snapshot() },
		Event: func(l *delivery.Lifecycle) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: l.ID()}
		},
	})
}

// changePhases applies a change to a Lifecycle's phases and checks the
// Environments they name.
func (s *service) changePhases(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, apply func(*delivery.Lifecycle) error) (*delivery.Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, delivery.EventLifecycleUpdated,
		func(ctx context.Context, l *delivery.Lifecycle) error {
			if err := apply(l); err != nil {
				return err
			}
			return s.checkEnvironments(ctx, l)
		})
}

// UpdateLifecycle changes an active Lifecycle's slug, name, or description.
func (s *service) UpdateLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props delivery.UpdateLifecycleProps) (*delivery.Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, delivery.EventLifecycleUpdated,
		func(_ context.Context, l *delivery.Lifecycle) error { return l.Update(props) })
}

// AddLifecyclePhase appends a phase to an active Lifecycle.
func (s *service) AddLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props delivery.LifecyclePhaseProps) (*delivery.Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID,
		func(l *delivery.Lifecycle) error { _, err := l.AddPhase(props); return err })
}

// UpdateLifecyclePhase replaces a phase's content.
func (s *service) UpdateLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, props delivery.LifecyclePhaseProps) (*delivery.Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID,
		func(l *delivery.Lifecycle) error { return l.UpdatePhase(phaseID, props) })
}

// MoveLifecyclePhase moves a phase to another position.
func (s *service) MoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, position int) (*delivery.Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID,
		func(l *delivery.Lifecycle) error { return l.MovePhase(phaseID, position) })
}

// RemoveLifecyclePhase removes a phase.
func (s *service) RemoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string) (*delivery.Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID,
		func(l *delivery.Lifecycle) error { return l.RemovePhase(phaseID) })
}

// ArchiveLifecycle archives a Lifecycle no active Channel of an active
// Application follows. An archived Application's Channels do not hold it;
// restoring that Application needs the Lifecycle restored first. Archiving
// an archived one does nothing.
func (s *service) ArchiveLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, delivery.EventLifecycleArchived,
		func(ctx context.Context, l *delivery.Lifecycle) error {
			if l.Archived() {
				return change.ErrUnchanged
			}
			n, err := s.channels.CountActiveChannelsWithLifecycle(ctx, organizationID, l.ID())
			if err != nil {
				return fmt.Errorf("count channels: %w", err)
			}
			if n > 0 {
				return fmt.Errorf("%w: %d active channels follow lifecycle %s", delivery.ErrInUse, n, l.ID())
			}
			l.Archive()
			return nil
		})
}

// RestoreLifecycle restores an archived Lifecycle in an active Workspace
// whose Environments are all still active.
func (s *service) RestoreLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, delivery.EventLifecycleRestored,
		func(ctx context.Context, l *delivery.Lifecycle) error {
			if !l.Archived() {
				return change.ErrUnchanged
			}
			if err := delivery.ActiveWorkspace(ctx, s.workspaces, organizationID, l.WorkspaceID()); err != nil {
				return err
			}
			if err := s.checkEnvironments(ctx, l); err != nil {
				return err
			}
			l.Restore()
			return nil
		})
}
