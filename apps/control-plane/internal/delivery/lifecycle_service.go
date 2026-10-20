package delivery

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// CreateLifecycleInput describes a new Lifecycle and its phases, in order.
type CreateLifecycleInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description string
	Phases      []LifecyclePhaseProps
}

// CreateLifecycle creates a Lifecycle in an active Workspace. Its phases may
// name only that Workspace's active Environments.
func (s *Service) CreateLifecycle(ctx context.Context, actor identity.Actor, in CreateLifecycleInput) (*Lifecycle, error) {
	phases := make([]NewLifecyclePhaseProps, len(in.Phases))
	for i, p := range in.Phases {
		phases[i] = NewLifecyclePhaseProps{Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs}
	}
	l, err := NewLifecycle(NewLifecycleProps{
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
	d, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionLifecycleManage, OrganizationID: in.OrganizationID,
		ResourceID: in.WorkspaceID, Action: ActionLifecycleCreate, Proposal: in,
	})
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.activeWorkspace(ctx, l.OrganizationID(), l.WorkspaceID()); err != nil {
			return err
		}
		if err := s.checkLifecycleEnvironments(ctx, l); err != nil {
			return err
		}
		if err := s.lifecycles.CreateLifecycle(ctx, l); err != nil {
			return fmt.Errorf("create lifecycle: %w", err)
		}
		return s.record(ctx, d, audit.Event{Type: EventLifecycleCreated, OrganizationID: l.OrganizationID(), ResourceID: l.ID(), Actor: actor, After: l.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// checkLifecycleEnvironments checks that every Environment in the
// Lifecycle's phases exists, is active, and is in the Lifecycle's Workspace.
func (s *Service) checkLifecycleEnvironments(ctx context.Context, l *Lifecycle) error {
	ids := l.EnvironmentIDs()
	if len(ids) == 0 {
		return nil
	}
	envs, err := s.environments.GetEnvironments(ctx, l.OrganizationID(), ids)
	if err != nil {
		return fmt.Errorf("get environments: %w", err)
	}
	byID := make(map[string]*Environment, len(envs))
	for _, e := range envs {
		byID[e.ID()] = e
	}
	for _, id := range ids {
		e, ok := byID[id]
		switch {
		case !ok:
			return fmt.Errorf("%w: environment %s", ErrNotFound, id)
		case e.WorkspaceID() != l.WorkspaceID():
			return fmt.Errorf("%w: environment %s is not in workspace %s", ErrWorkspaceMismatch, id, l.WorkspaceID())
		case e.Archived():
			return fmt.Errorf("%w: environment %s", ErrArchived, id)
		}
	}
	return nil
}

// GetLifecycle returns a Lifecycle if the actor may read its Workspace's Lifecycles.
func (s *Service) GetLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error) {
	l, err := s.lifecycles.GetLifecycle(ctx, organizationID, lifecycleID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionLifecycleRead, OrganizationID: organizationID,
		ResourceID: l.WorkspaceID(), Action: ActionLifecycleRead,
	}); err != nil {
		return nil, err
	}
	return l, nil
}

// ListLifecycles lists a Workspace's Lifecycles.
func (s *Service) ListLifecycles(ctx context.Context, actor identity.Actor, query ListLifecyclesQuery) (*pagination.Result[*Lifecycle], error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionLifecycleRead, OrganizationID: query.OrganizationID,
		ResourceID: query.WorkspaceID, Action: ActionLifecycleList,
	}); err != nil {
		return nil, err
	}
	return s.lifecycles.ListLifecycles(ctx, query)
}

func (s *Service) changeLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, action, event string, proposal any, apply func(context.Context, *Lifecycle) error) (*Lifecycle, error) {
	return run(ctx, s, actor, change[*Lifecycle]{
		load: func(ctx context.Context) (*Lifecycle, error) {
			return s.lifecycles.GetLifecycle(ctx, organizationID, lifecycleID)
		},
		// Locked so a Channel cannot start following it while it is archived.
		lock: func(ctx context.Context) (*Lifecycle, error) {
			return s.lifecycles.GetLifecycleForUpdate(ctx, organizationID, lifecycleID)
		},
		authorize: func(ctx context.Context, l *Lifecycle) (authorization.Decision, error) {
			return s.authorize(ctx, authorization.Request{
				Actor: actor, Permission: PermissionLifecycleManage, OrganizationID: organizationID,
				ResourceID: l.WorkspaceID(), Action: action, Proposal: proposal,
			})
		},
		apply:    apply,
		save:     s.lifecycles.UpdateLifecycle,
		snapshot: func(l *Lifecycle) any { return l.Snapshot() },
		event: func(l *Lifecycle) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: l.ID()}
		},
	})
}

// changePhases applies a change to a Lifecycle's phases and checks the
// Environments they name.
func (s *Service) changePhases(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, action string, proposal any, apply func(*Lifecycle) error) (*Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, action, EventLifecycleUpdated, proposal,
		func(ctx context.Context, l *Lifecycle) error {
			if err := apply(l); err != nil {
				return err
			}
			return s.checkLifecycleEnvironments(ctx, l)
		})
}

// UpdateLifecycle changes an active Lifecycle's slug, name, or description.
func (s *Service) UpdateLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props UpdateLifecycleProps) (*Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, ActionLifecycleUpdate, EventLifecycleUpdated, props,
		func(_ context.Context, l *Lifecycle) error { return l.Update(props) })
}

// AddLifecyclePhase appends a phase to an active Lifecycle.
func (s *Service) AddLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props LifecyclePhaseProps) (*Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID, ActionLifecycleAddPhase, props,
		func(l *Lifecycle) error { _, err := l.AddPhase(props); return err })
}

// UpdateLifecyclePhase replaces a phase's content.
func (s *Service) UpdateLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, props LifecyclePhaseProps) (*Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID, ActionLifecycleUpdatePhase, props,
		func(l *Lifecycle) error { return l.UpdatePhase(phaseID, props) })
}

// MoveLifecyclePhase moves a phase to another position.
func (s *Service) MoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, position int) (*Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID, ActionLifecycleMovePhase, position,
		func(l *Lifecycle) error { return l.MovePhase(phaseID, position) })
}

// RemoveLifecyclePhase removes a phase.
func (s *Service) RemoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string) (*Lifecycle, error) {
	return s.changePhases(ctx, actor, organizationID, lifecycleID, ActionLifecycleRemovePhase, phaseID,
		func(l *Lifecycle) error { return l.RemovePhase(phaseID) })
}

// ArchiveLifecycle archives a Lifecycle no active Channel of an active
// Application follows. An archived Application's Channels do not hold it;
// restoring that Application needs the Lifecycle restored first.
func (s *Service) ArchiveLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, ActionLifecycleArchive, EventLifecycleArchived, nil,
		func(ctx context.Context, l *Lifecycle) error {
			if l.Archived() {
				return errUnchanged
			}
			n, err := s.applications.CountActiveChannelsWithLifecycle(ctx, organizationID, l.ID())
			if err != nil {
				return fmt.Errorf("count channels: %w", err)
			}
			if n > 0 {
				return fmt.Errorf("%w: %d active channels follow lifecycle %s", ErrInUse, n, l.ID())
			}
			l.Archive()
			return nil
		})
}

// RestoreLifecycle restores an archived Lifecycle in an active Workspace
// whose Environments are all still active.
func (s *Service) RestoreLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error) {
	return s.changeLifecycle(ctx, actor, organizationID, lifecycleID, ActionLifecycleRestore, EventLifecycleRestored, nil,
		func(ctx context.Context, l *Lifecycle) error {
			if !l.Archived() {
				return errUnchanged
			}
			if err := s.activeWorkspace(ctx, organizationID, l.WorkspaceID()); err != nil {
				return err
			}
			if err := s.checkLifecycleEnvironments(ctx, l); err != nil {
				return err
			}
			l.Restore()
			return nil
		})
}
