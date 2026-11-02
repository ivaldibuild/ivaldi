package delivery

import (
	"context"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// CreateEnvironmentInput describes a new Environment.
type CreateEnvironmentInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug             string
	DisplayName      string
	Tier             Tier
	ExecutionEnabled bool
}

// CreateEnvironment creates an Environment in an active Workspace and
// registers its IAM resource under the Workspace.
func (s *Service) CreateEnvironment(ctx context.Context, actor identity.Actor, in CreateEnvironmentInput) (*Environment, error) {
	e, err := NewEnvironment(NewEnvironmentProps{
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
	d, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: in.OrganizationID,
		ResourceID: in.WorkspaceID, Action: ActionEnvironmentCreate, Proposal: in,
	})
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.activeWorkspace(ctx, e.OrganizationID(), e.WorkspaceID()); err != nil {
			return err
		}
		if err := s.environments.CreateEnvironment(ctx, e); err != nil {
			return fmt.Errorf("create environment: %w", err)
		}
		if err := s.recordEvents(ctx, AggregateEnvironment, e.ID(), e); err != nil {
			return err
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{ID: e.ID(), OrganizationID: e.OrganizationID(), Type: iam.ResourceEnvironment, ParentID: e.WorkspaceID()}); err != nil {
			return fmt.Errorf("register environment resource: %w", err)
		}
		return s.record(ctx, d, audit.Event{Type: EventEnvironmentCreated, OrganizationID: e.OrganizationID(), ResourceID: e.ID(), Actor: actor, After: e.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetEnvironment returns an Environment the actor may read.
func (s *Service) GetEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string) (*Environment, error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentRead, OrganizationID: organizationID,
		ResourceID: environmentID, Action: ActionEnvironmentRead,
	}); err != nil {
		return nil, err
	}
	return s.environments.GetEnvironment(ctx, organizationID, environmentID)
}

// GetEnvironmentBySlug returns the active Environment with the slug in a
// Workspace, if the actor may read it. It is authorized on the Environment
// it finds, so it can tell an Organization member that a slug exists.
func (s *Service) GetEnvironmentBySlug(ctx context.Context, actor identity.Actor, organizationID, workspaceID, slug string) (*Environment, error) {
	e, err := s.environments.GetEnvironmentBySlug(ctx, organizationID, workspaceID, slug)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentRead, OrganizationID: organizationID,
		ResourceID: e.ID(), Action: ActionEnvironmentRead,
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// ListEnvironments lists a Workspace's Environments. It needs read access on
// the Workspace; listing only the Environments each caller can see waits for
// IAM's resource listing.
func (s *Service) ListEnvironments(ctx context.Context, actor identity.Actor, query ListEnvironmentsQuery) (*pagination.Result[*Environment], error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentRead, OrganizationID: query.OrganizationID,
		ResourceID: query.WorkspaceID, Action: ActionEnvironmentList,
	}); err != nil {
		return nil, err
	}
	return s.environments.ListEnvironments(ctx, query)
}

// UpdateEnvironment changes an active Environment.
func (s *Service) UpdateEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string, props UpdateEnvironmentProps) (*Environment, error) {
	return run(ctx, s, actor, change[*Environment]{
		aggregateType: AggregateEnvironment,
		aggregateID:   func(e *Environment) string { return e.ID() },
		load: func(ctx context.Context) (*Environment, error) {
			return s.environments.GetEnvironment(ctx, organizationID, environmentID)
		},
		authorize: func(ctx context.Context, e *Environment) (authorization.Decision, error) {
			return s.authorize(ctx, authorization.Request{
				Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: organizationID,
				ResourceID: e.ID(), Action: ActionEnvironmentUpdate, Proposal: props,
			})
		},
		apply:    func(_ context.Context, e *Environment) error { return e.Update(props) },
		save:     s.environments.UpdateEnvironment,
		snapshot: func(e *Environment) any { return e.Snapshot() },
		event: func(e *Environment) audit.Event {
			return audit.Event{Type: EventEnvironmentUpdated, OrganizationID: organizationID, ResourceID: e.ID()}
		},
	})
}

// ArchiveEnvironments archives Environments that have no active Targets and
// are in no active Lifecycle, all together or none of them. Archived ones
// are left as they are.
func (s *Service) ArchiveEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*Environment, error) {
	ids := uniqueIDs(environmentIDs)
	decisions := make(map[string]authorization.Decision, len(ids))
	for _, id := range ids {
		d, err := s.authorize(ctx, authorization.Request{
			Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: organizationID,
			ResourceID: id, Action: ActionEnvironmentArchive,
		})
		if err != nil {
			return nil, err
		}
		decisions[id] = d
	}

	var out []*Environment
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
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
			if err := s.checkEnvironmentUnused(ctx, e); err != nil {
				return err
			}
			before := e.Snapshot()
			e.Archive()
			if err := s.environments.UpdateEnvironment(ctx, e); err != nil {
				return fmt.Errorf("archive environment %s: %w", id, err)
			}
			if err := s.recordEvents(ctx, AggregateEnvironment, e.ID(), e); err != nil {
				return err
			}
			if err := s.resources.ArchiveResource(ctx, organizationID, id); err != nil {
				return fmt.Errorf("archive environment resource %s: %w", id, err)
			}
			if err := s.record(ctx, decisions[id], audit.Event{Type: EventEnvironmentArchived, OrganizationID: organizationID, ResourceID: id, Actor: actor, Before: before, After: e.Snapshot()}); err != nil {
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

func (s *Service) checkEnvironmentUnused(ctx context.Context, e *Environment) error {
	n, err := s.targets.CountActiveTargets(ctx, e.OrganizationID(), e.ID())
	if err != nil {
		return fmt.Errorf("count targets: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: environment %s has %d active targets", ErrInUse, e.ID(), n)
	}
	n, err = s.lifecycles.CountActiveLifecyclesWithEnvironment(ctx, e.OrganizationID(), e.ID())
	if err != nil {
		return fmt.Errorf("count lifecycles: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: environment %s is in %d active lifecycles", ErrInUse, e.ID(), n)
	}
	return nil
}

// RestoreEnvironments restores archived Environments in active Workspaces,
// all together or none of them. Each is authorized on its Workspace, because
// grants on an archived Environment do not apply.
func (s *Service) RestoreEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*Environment, error) {
	ids := uniqueIDs(environmentIDs)
	found, err := s.environments.GetEnvironments(ctx, organizationID, ids)
	if err != nil {
		return nil, err
	}
	workspaceOf := make(map[string]string, len(found))
	for _, e := range found {
		workspaceOf[e.ID()] = e.WorkspaceID()
	}
	decisions := make(map[string]authorization.Decision, len(ids))
	for _, id := range ids {
		ws, ok := workspaceOf[id]
		if !ok {
			return nil, fmt.Errorf("%w: environment %s", ErrNotFound, id)
		}
		d, err := s.authorize(ctx, authorization.Request{
			Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: organizationID,
			ResourceID: ws, Action: ActionEnvironmentRestore, Proposal: id,
		})
		if err != nil {
			return nil, err
		}
		decisions[id] = d
	}

	var out []*Environment
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
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
			if err := s.activeWorkspace(ctx, organizationID, e.WorkspaceID()); err != nil {
				return err
			}
			before := e.Snapshot()
			e.Restore()
			if err := s.environments.UpdateEnvironment(ctx, e); err != nil {
				return fmt.Errorf("restore environment %s: %w", id, err)
			}
			if err := s.recordEvents(ctx, AggregateEnvironment, e.ID(), e); err != nil {
				return err
			}
			if err := s.resources.RestoreResource(ctx, organizationID, id); err != nil {
				return fmt.Errorf("restore environment resource %s: %w", id, err)
			}
			if err := s.record(ctx, decisions[id], audit.Event{Type: EventEnvironmentRestored, OrganizationID: organizationID, ResourceID: id, Actor: actor, Before: before, After: e.Snapshot()}); err != nil {
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

// CreateTargetInput describes a new Target.
type CreateTargetInput struct {
	OrganizationID string
	EnvironmentID  string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Runtime     Runtime
	Locator     map[string]string
	// AgentPrincipalID is the runtime agent that serves the Target, if known yet.
	AgentPrincipalID string
}

// CreateTarget adds a Target to an active Environment.
func (s *Service) CreateTarget(ctx context.Context, actor identity.Actor, in CreateTargetInput) (*Target, error) {
	t, err := NewTarget(NewTargetProps{
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
	d, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: in.OrganizationID,
		ResourceID: in.EnvironmentID, Action: ActionTargetCreate, Proposal: in,
	})
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so the Environment cannot be archived while the Target is added.
		e, err := s.environments.GetEnvironmentForUpdate(ctx, in.OrganizationID, in.EnvironmentID)
		if err != nil {
			return err
		}
		if e.Archived() {
			return fmt.Errorf("%w: environment %s", ErrArchived, e.ID())
		}
		if err := s.targets.CreateTarget(ctx, t); err != nil {
			return fmt.Errorf("create target: %w", err)
		}
		if err := s.recordEvents(ctx, AggregateTarget, t.ID(), t); err != nil {
			return err
		}
		return s.record(ctx, d, audit.Event{Type: EventTargetCreated, OrganizationID: t.OrganizationID(), ResourceID: t.ID(), Actor: actor, After: t.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// GetTarget returns a Target if the actor may read its Environment.
func (s *Service) GetTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error) {
	t, err := s.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentRead, OrganizationID: organizationID,
		ResourceID: t.EnvironmentID(), Action: ActionTargetRead,
	}); err != nil {
		return nil, err
	}
	return t, nil
}

// ListTargets lists an Environment's Targets.
func (s *Service) ListTargets(ctx context.Context, actor identity.Actor, query ListTargetsQuery) (*pagination.Result[*Target], error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionEnvironmentRead, OrganizationID: query.OrganizationID,
		ResourceID: query.EnvironmentID, Action: ActionTargetList,
	}); err != nil {
		return nil, err
	}
	return s.targets.ListTargets(ctx, query)
}

func (s *Service) changeTarget(ctx context.Context, actor identity.Actor, organizationID, targetID, action, event string, proposal any, apply func(context.Context, *Target) error) (*Target, error) {
	return run(ctx, s, actor, change[*Target]{
		aggregateType: AggregateTarget,
		aggregateID:   func(t *Target) string { return t.ID() },
		load: func(ctx context.Context) (*Target, error) {
			return s.targets.GetTarget(ctx, organizationID, targetID)
		},
		authorize: func(ctx context.Context, t *Target) (authorization.Decision, error) {
			return s.authorize(ctx, authorization.Request{
				Actor: actor, Permission: PermissionEnvironmentManage, OrganizationID: organizationID,
				ResourceID: t.EnvironmentID(), Action: action, Proposal: proposal,
			})
		},
		apply:    apply,
		save:     s.targets.UpdateTarget,
		snapshot: func(t *Target) any { return t.Snapshot() },
		event: func(t *Target) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: t.ID()}
		},
	})
}

// UpdateTarget changes an active Target.
func (s *Service) UpdateTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string, props UpdateTargetProps) (*Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, ActionTargetUpdate, EventTargetUpdated, props,
		func(_ context.Context, t *Target) error { return t.Update(props) })
}

// AssignTargetAgent makes principalID the Target's runtime agent, or
// unassigns the agent when principalID is empty.
func (s *Service) AssignTargetAgent(ctx context.Context, actor identity.Actor, organizationID, targetID, principalID string) (*Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, ActionTargetAssignAgent, EventTargetAgentAssigned, principalID,
		func(_ context.Context, t *Target) error {
			if principalID == "" {
				if t.AgentPrincipalID() == nil && !t.Archived() {
					return errUnchanged
				}
				return t.UnassignAgent()
			}
			return t.AssignAgent(principalID)
		})
}

// RecordTargetReport notes that the Target's agent is in contact. The actor
// must be the assigned agent, which is the only authorization it needs. It
// records no audit event: reports are frequent, and change no configuration.
func (s *Service) RecordTargetReport(ctx context.Context, actor identity.Actor, organizationID, targetID string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
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
func (s *Service) ArchiveTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, ActionTargetArchive, EventTargetArchived, nil,
		func(_ context.Context, t *Target) error {
			if t.Archived() {
				return errUnchanged
			}
			t.Archive()
			return nil
		})
}

// RestoreTarget restores an archived Target in an active Environment.
func (s *Service) RestoreTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error) {
	return s.changeTarget(ctx, actor, organizationID, targetID, ActionTargetRestore, EventTargetRestored, nil,
		func(ctx context.Context, t *Target) error {
			if !t.Archived() {
				return errUnchanged
			}
			e, err := s.environments.GetEnvironmentForUpdate(ctx, organizationID, t.EnvironmentID())
			if err != nil {
				return err
			}
			if e.Archived() {
				return fmt.Errorf("%w: environment %s", ErrArchived, e.ID())
			}
			t.Restore()
			return nil
		})
}
