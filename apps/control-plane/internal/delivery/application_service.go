package delivery

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// CreateApplicationInput describes a new Application and the Lifecycle its
// default Channel follows.
type CreateApplicationInput struct {
	OrganizationID string
	ProjectID      string
	// OwnerTeamID defaults to the Project's owning Team.
	OwnerTeamID string
	// Slug is derived from DisplayName when empty.
	Slug             string
	DisplayName      string
	SourceID         string
	WorkloadSelector map[string]string
	// DefaultLifecycleID is the Lifecycle the default Channel follows. It
	// must be active and in the Project's Workspace.
	DefaultLifecycleID string
}

// CreateApplication creates an Application in an active Project, with its
// default Channel. The Application's Workspace is copied from the Project.
func (s *service) CreateApplication(ctx context.Context, actor identity.Actor, in CreateApplicationInput) (*Application, *Channel, error) {
	if !id.Valid(Lifecycle{}.Prefix(), in.DefaultLifecycleID) {
		return nil, nil, NewErrInvalidInput("an application needs the lifecycle its default channel follows")
	}
	// NewApplication needs the Project's Workspace, so it runs in the
	// transaction; these checks refuse bad input before authorizing.
	if err := validateDisplayName(in.DisplayName); err != nil {
		return nil, nil, err
	}
	if _, err := slugOrDerived(in.Slug, in.DisplayName); err != nil {
		return nil, nil, err
	}
	if len(in.WorkloadSelector) > 0 {
		if err := validateSelector(in.WorkloadSelector); err != nil {
			return nil, nil, err
		}
	}
	d, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionApplicationManage, OrganizationID: in.OrganizationID,
		ResourceID: in.ProjectID, Action: ActionApplicationCreate, Proposal: in,
	})
	if err != nil {
		return nil, nil, err
	}

	var app *Application
	var ch *Channel
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.activeProject(ctx, in.OrganizationID, in.ProjectID)
		if err != nil {
			return err
		}
		owner := in.OwnerTeamID
		if owner == "" {
			owner = p.OwnerTeamID()
		}
		if err := s.checkTeam(ctx, in.OrganizationID, owner); err != nil {
			return err
		}
		app, err = NewApplication(NewApplicationProps{
			OrganizationID:   in.OrganizationID,
			WorkspaceID:      p.WorkspaceID(),
			ProjectID:        p.ID(),
			OwnerTeamID:      owner,
			Slug:             in.Slug,
			DisplayName:      in.DisplayName,
			SourceID:         &in.SourceID,
			WorkloadSelector: in.WorkloadSelector,
		}, nil)
		if err != nil {
			return err
		}
		l, err := s.lifecycleFor(ctx, in.OrganizationID, app.WorkspaceID(), in.DefaultLifecycleID)
		if err != nil {
			return err
		}
		ch, err = NewChannel(NewChannelProps{
			OrganizationID: in.OrganizationID,
			ApplicationID:  app.ID(),
			LifecycleID:    l.ID(),
			Slug:           DefaultChannelSlug,
			DisplayName:    DefaultChannelName,
			IsDefault:      true,
		}, nil)
		if err != nil {
			return err
		}
		if err := s.applications.CreateApplication(ctx, app); err != nil {
			return fmt.Errorf("create application: %w", err)
		}
		if err := s.applications.CreateChannel(ctx, ch); err != nil {
			return fmt.Errorf("create default channel: %w", err)
		}
		if err := s.recordEvents(ctx, AggregateApplication, app.ID(), app); err != nil {
			return err
		}
		if err := s.recordEvents(ctx, AggregateChannel, ch.ID(), ch); err != nil {
			return err
		}
		if err := s.record(ctx, d, audit.Event{Type: EventApplicationCreated, OrganizationID: in.OrganizationID, ResourceID: app.ID(), Actor: actor, After: app.Snapshot()}); err != nil {
			return err
		}
		return s.record(ctx, d, audit.Event{Type: EventChannelCreated, OrganizationID: in.OrganizationID, ResourceID: ch.ID(), Actor: actor, After: ch.Snapshot()})
	})
	if err != nil {
		return nil, nil, err
	}
	return app, ch, nil
}

// lifecycleFor returns an active Lifecycle in the Workspace, locked so it
// cannot be archived while a Channel starts following it.
func (s *service) lifecycleFor(ctx context.Context, organizationID, workspaceID, lifecycleID string) (*Lifecycle, error) {
	l, err := s.lifecycles.GetLifecycleForUpdate(ctx, organizationID, lifecycleID)
	if err != nil {
		return nil, err
	}
	if l.WorkspaceID() != workspaceID {
		return nil, fmt.Errorf("%w: lifecycle %s is not in workspace %s", ErrWorkspaceMismatch, lifecycleID, workspaceID)
	}
	if l.Archived() {
		return nil, fmt.Errorf("%w: lifecycle %s", ErrArchived, lifecycleID)
	}
	return l, nil
}

// registryFor returns an active Registry in the Workspace.
func (s *service) registryFor(ctx context.Context, organizationID, workspaceID, registryID string) (*Registry, error) {
	r, err := s.registries.GetRegistry(ctx, organizationID, registryID)
	if err != nil {
		return nil, err
	}
	if r.WorkspaceID() != workspaceID {
		return nil, fmt.Errorf("%w: registry %s is not in workspace %s", ErrWorkspaceMismatch, registryID, workspaceID)
	}
	if r.Archived() {
		return nil, fmt.Errorf("%w: registry %s", ErrArchived, registryID)
	}
	return r, nil
}

// activeApplication returns an active Application.
func (s *service) activeApplication(ctx context.Context, organizationID, applicationID string) (*Application, error) {
	a, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return nil, err
	}
	if a.Archived() {
		return nil, fmt.Errorf("%w: application %s", ErrArchived, applicationID)
	}
	return a, nil
}

// authorizeApplication checks a permission on the Application's Project.
func (s *service) authorizeApplication(ctx context.Context, actor identity.Actor, permission, action string, organizationID, applicationID string, proposal any) (authorization.Decision, error) {
	a, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return authorization.Decision{}, err
	}
	return s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: a.ProjectID(), Action: action, Proposal: proposal,
	})
}

// GetApplication returns an Application if the actor may read its Project's Applications.
func (s *service) GetApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error) {
	a, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionApplicationRead, OrganizationID: organizationID,
		ResourceID: a.ProjectID(), Action: ActionApplicationRead,
	}); err != nil {
		return nil, err
	}
	return a, nil
}

// ListApplications lists a Project's Applications, or a Workspace's when no
// Project is given, authorized on whichever is given.
func (s *service) ListApplications(ctx context.Context, actor identity.Actor, query ListApplicationsQuery) (*pagination.Result[*Application], error) {
	resource := query.ProjectID
	if resource == "" {
		resource = query.WorkspaceID
	}
	if resource == "" {
		return nil, NewErrInvalidInput("listing applications needs a project or a workspace")
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionApplicationRead, OrganizationID: query.OrganizationID,
		ResourceID: resource, Action: ActionApplicationList,
	}); err != nil {
		return nil, err
	}
	return s.applications.ListApplications(ctx, query)
}

func (s *service) changeApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID, action, event string, proposal any, apply func(context.Context, *Application) error) (*Application, error) {
	return run(ctx, s, actor, change[*Application]{
		aggregateType: AggregateApplication,
		aggregateID:   func(a *Application) string { return a.ID() },
		load: func(ctx context.Context) (*Application, error) {
			return s.applications.GetApplication(ctx, organizationID, applicationID)
		},
		lock: func(ctx context.Context) (*Application, error) {
			return s.applications.GetApplicationForUpdate(ctx, organizationID, applicationID)
		},
		authorize: func(ctx context.Context, a *Application) (authorization.Decision, error) {
			return s.authorize(ctx, authorization.Request{
				Actor: actor, Permission: PermissionApplicationManage, OrganizationID: organizationID,
				ResourceID: a.ProjectID(), Action: action, Proposal: proposal,
			})
		},
		apply:    apply,
		save:     s.applications.UpdateApplication,
		snapshot: func(a *Application) any { return a.Snapshot() },
		event: func(a *Application) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
}

// UpdateApplication changes an active Application's slug, name, or workload selector.
func (s *service) UpdateApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string, props UpdateApplicationProps) (*Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, ActionApplicationUpdate, EventApplicationUpdated, props,
		func(_ context.Context, a *Application) error { return a.Update(props) })
}

// LinkApplicationSource records the Source the Application is built from,
// or removes it when sourceID is empty.
func (s *service) LinkApplicationSource(ctx context.Context, actor identity.Actor, organizationID, applicationID, sourceID string) (*Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, ActionApplicationLinkSource, EventApplicationUpdated, sourceID,
		func(_ context.Context, a *Application) error {
			if sourceID == "" {
				return a.UnlinkSource()
			}
			return a.LinkSource(sourceID)
		})
}

// TransferApplication hands an Application to another active Team.
func (s *service) TransferApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID, ownerTeamID string) (*Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, ActionApplicationTransfer, EventApplicationOwnerChanged, ownerTeamID,
		func(ctx context.Context, a *Application) error {
			if err := s.checkTeam(ctx, organizationID, ownerTeamID); err != nil {
				return err
			}
			return a.TransferTo(ownerTeamID)
		})
}

// ArchiveApplication archives an Application. Its Channels, artifacts, and
// placements stay as they are, so restoring it brings them back; until then
// they cannot be changed.
func (s *service) ArchiveApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, ActionApplicationArchive, EventApplicationArchived, nil,
		func(_ context.Context, a *Application) error {
			if a.Archived() {
				return errUnchanged
			}
			a.Archive()
			return nil
		})
}

// RestoreApplication restores an archived Application in an active Project
// whose active Channels' Lifecycles are all active.
func (s *service) RestoreApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, ActionApplicationRestore, EventApplicationRestored, nil,
		func(ctx context.Context, a *Application) error {
			if !a.Archived() {
				return errUnchanged
			}
			if _, err := s.activeProject(ctx, organizationID, a.ProjectID()); err != nil {
				return err
			}
			// Lifecycles can be archived while the Application is, so its
			// Channels' Lifecycles must be restored first.
			channels, err := s.applications.ListChannels(ctx, organizationID, a.ID(), false)
			if err != nil {
				return fmt.Errorf("list channels: %w", err)
			}
			for _, c := range channels {
				if _, err := s.lifecycleFor(ctx, organizationID, a.WorkspaceID(), c.LifecycleID()); err != nil {
					return fmt.Errorf("channel %s: %w", c.ID(), err)
				}
			}
			a.Restore()
			return nil
		})
}

// CreateArtifactInput describes an artifact someone declares for an Application.
type CreateArtifactInput struct {
	OrganizationID string
	ApplicationID  string
	Name           string
	ArtifactKind   ArtifactKind
	ArtifactName   string
	// RegistryID must be an active Registry in the Application's Workspace.
	RegistryID string
	Required   bool
}

// CreateArtifact declares an artifact for an active Application.
func (s *service) CreateArtifact(ctx context.Context, actor identity.Actor, in CreateArtifactInput) (*Artifact, error) {
	a, err := NewArtifact(NewArtifactProps{
		OrganizationID: in.OrganizationID,
		ApplicationID:  in.ApplicationID,
		Name:           in.Name,
		ArtifactKind:   in.ArtifactKind,
		ArtifactName:   in.ArtifactName,
		RegistryID:     &in.RegistryID,
		Required:       in.Required,
		Source:         ArtifactDeclared,
	}, nil)
	if err != nil {
		return nil, err
	}
	d, err := s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionArtifactCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.activeApplication(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		if r := a.RegistryID(); r != nil {
			if _, err := s.registryFor(ctx, in.OrganizationID, app.WorkspaceID(), *r); err != nil {
				return err
			}
		}
		if err := s.applications.CreateArtifact(ctx, a); err != nil {
			return fmt.Errorf("create artifact: %w", err)
		}
		if err := s.recordEvents(ctx, AggregateArtifact, a.ID(), a); err != nil {
			return err
		}
		return s.record(ctx, d, audit.Event{Type: EventArtifactCreated, OrganizationID: in.OrganizationID, ResourceID: a.ID(), Actor: actor, After: a.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListArtifacts lists an Application's artifacts.
func (s *service) ListArtifacts(ctx context.Context, actor identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*Artifact, error) {
	if _, err := s.authorizeApplication(ctx, actor, PermissionApplicationRead, ActionArtifactList, organizationID, applicationID, nil); err != nil {
		return nil, err
	}
	return s.applications.ListArtifacts(ctx, organizationID, applicationID, includeArchived)
}

func (s *service) changeArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID, action, event string, proposal any, apply func(context.Context, *Artifact) error) (*Artifact, error) {
	return run(ctx, s, actor, change[*Artifact]{
		aggregateType: AggregateArtifact,
		aggregateID:   func(a *Artifact) string { return a.ID() },
		load: func(ctx context.Context) (*Artifact, error) {
			return s.applications.GetArtifact(ctx, organizationID, artifactID)
		},
		authorize: func(ctx context.Context, a *Artifact) (authorization.Decision, error) {
			return s.authorizeApplication(ctx, actor, PermissionApplicationManage, action, organizationID, a.ApplicationID(), proposal)
		},
		// An archived Application's artifacts change only once it is restored.
		apply: func(ctx context.Context, a *Artifact) error {
			if _, err := s.activeApplication(ctx, organizationID, a.ApplicationID()); err != nil {
				return err
			}
			return apply(ctx, a)
		},
		save:     s.applications.UpdateArtifact,
		snapshot: func(a *Artifact) any { return a.Snapshot() },
		event: func(a *Artifact) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
}

// UpdateArtifact changes an active artifact. Renaming one that a placement
// applies Releases from is refused, because the placement refers to it by name.
func (s *service) UpdateArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string, props UpdateArtifactProps) (*Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, ActionArtifactUpdate, EventArtifactUpdated, props,
		func(ctx context.Context, a *Artifact) error {
			if props.Name != nil && *props.Name != a.Name() {
				if err := s.checkNotDefinition(ctx, a); err != nil {
					return err
				}
			}
			return a.Update(props)
		})
}

// SetArtifactRegistry records the Registry an artifact is looked up in, or
// removes it when registryID is empty.
func (s *service) SetArtifactRegistry(ctx context.Context, actor identity.Actor, organizationID, artifactID, registryID string) (*Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, ActionArtifactSetRegistry, EventArtifactUpdated, registryID,
		func(ctx context.Context, a *Artifact) error {
			if registryID == "" {
				return a.ClearRegistry()
			}
			app, err := s.applications.GetApplication(ctx, organizationID, a.ApplicationID())
			if err != nil {
				return err
			}
			if _, err := s.registryFor(ctx, organizationID, app.WorkspaceID(), registryID); err != nil {
				return err
			}
			return a.UseRegistry(registryID)
		})
}

// DeclareArtifact confirms an artifact the runtime agent observed.
func (s *service) DeclareArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, ActionArtifactDeclare, EventArtifactUpdated, nil,
		func(_ context.Context, a *Artifact) error { return a.Declare() })
}

// ArchiveArtifact archives an artifact that no active Channel's rules are
// scoped to and no placement applies Releases from.
func (s *service) ArchiveArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, ActionArtifactArchive, EventArtifactArchived, nil,
		func(ctx context.Context, a *Artifact) error {
			if a.Archived() {
				return errUnchanged
			}
			channels, err := s.applications.ListChannels(ctx, organizationID, a.ApplicationID(), false)
			if err != nil {
				return fmt.Errorf("list channels: %w", err)
			}
			for _, c := range channels {
				for _, r := range c.Rules() {
					if r.ArtifactID != nil && *r.ArtifactID == a.ID() {
						return fmt.Errorf("%w: rules of channel %s are scoped to artifact %s", ErrInUse, c.ID(), a.ID())
					}
				}
			}
			if err := s.checkNotDefinition(ctx, a); err != nil {
				return err
			}
			a.Archive()
			return nil
		})
}

// checkNotDefinition refuses when a placement applies Releases from the artifact.
func (s *service) checkNotDefinition(ctx context.Context, a *Artifact) error {
	placements, err := s.applications.ListPlacements(ctx, a.OrganizationID(), a.ApplicationID())
	if err != nil {
		return fmt.Errorf("list placements: %w", err)
	}
	for _, p := range placements {
		if c := p.ApplyConfig(); c != nil && c.DefinitionArtifact == a.Name() {
			return fmt.Errorf("%w: target %s applies releases from artifact %q", ErrInUse, p.TargetID(), a.Name())
		}
	}
	return nil
}

// RestoreArtifact restores an archived artifact of an active Application.
func (s *service) RestoreArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, ActionArtifactRestore, EventArtifactRestored, nil,
		func(ctx context.Context, a *Artifact) error {
			if !a.Archived() {
				return errUnchanged
			}
			if _, err := s.activeApplication(ctx, organizationID, a.ApplicationID()); err != nil {
				return err
			}
			a.Restore()
			return nil
		})
}

// CreatePlacementInput places an Application on a Target.
type CreatePlacementInput struct {
	OrganizationID string
	ApplicationID  string
	TargetID       string
	// ApplyConfig says how Ivaldi applies Releases there; nil means
	// Deployments there are tracked only, until it is configured.
	ApplyConfig *ApplyConfig
}

// CreatePlacement places an active Application on an active Target in its Workspace.
func (s *service) CreatePlacement(ctx context.Context, actor identity.Actor, in CreatePlacementInput) (*Placement, error) {
	p, err := NewPlacement(NewPlacementProps{
		OrganizationID: in.OrganizationID,
		ApplicationID:  in.ApplicationID,
		TargetID:       in.TargetID,
		Source:         PlacementConfigured,
		ApplyConfig:    in.ApplyConfig,
	})
	if err != nil {
		return nil, err
	}
	d, err := s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionPlacementCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.activeApplication(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		t, err := s.targets.GetTarget(ctx, in.OrganizationID, in.TargetID)
		if err != nil {
			return err
		}
		if t.Archived() {
			return fmt.Errorf("%w: target %s", ErrArchived, t.ID())
		}
		e, err := s.environments.GetEnvironment(ctx, in.OrganizationID, t.EnvironmentID())
		if err != nil {
			return err
		}
		if e.WorkspaceID() != app.WorkspaceID() {
			return fmt.Errorf("%w: target %s is not in workspace %s", ErrWorkspaceMismatch, t.ID(), app.WorkspaceID())
		}
		if e.Archived() {
			return fmt.Errorf("%w: environment %s", ErrArchived, e.ID())
		}
		if c := p.ApplyConfig(); c != nil {
			if err := s.checkDefinitionArtifact(ctx, in.OrganizationID, in.ApplicationID, c.DefinitionArtifact); err != nil {
				return err
			}
		}
		if err := s.applications.CreatePlacement(ctx, p); err != nil {
			return fmt.Errorf("create placement: %w", err)
		}
		if err := s.recordEvents(ctx, AggregatePlacement, p.Key(), p); err != nil {
			return err
		}
		return s.record(ctx, d, audit.Event{Type: EventPlacementCreated, OrganizationID: in.OrganizationID, ResourceID: in.ApplicationID, Actor: actor, After: p.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// checkDefinitionArtifact checks that the Application has an active artifact with the name.
func (s *service) checkDefinitionArtifact(ctx context.Context, organizationID, applicationID, name string) error {
	artifacts, err := s.applications.ListArtifacts(ctx, organizationID, applicationID, false)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}
	for _, a := range artifacts {
		if a.Name() == name {
			return nil
		}
	}
	return fmt.Errorf("%w: application %s has no active artifact named %q", ErrNotFound, applicationID, name)
}

// ListPlacements lists the Targets an Application is placed on.
func (s *service) ListPlacements(ctx context.Context, actor identity.Actor, organizationID, applicationID string) ([]*Placement, error) {
	if _, err := s.authorizeApplication(ctx, actor, PermissionApplicationRead, ActionPlacementList, organizationID, applicationID, nil); err != nil {
		return nil, err
	}
	return s.applications.ListPlacements(ctx, organizationID, applicationID)
}

// ConfigurePlacement sets how Releases are applied to the Target, or removes
// it when config is nil so Deployments there are tracked only.
func (s *service) ConfigurePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string, config *ApplyConfig) (*Placement, error) {
	if config != nil {
		if err := config.validate(); err != nil {
			return nil, err
		}
	}
	return run(ctx, s, actor, change[*Placement]{
		aggregateType: AggregatePlacement,
		aggregateID:   (*Placement).Key,
		load: func(ctx context.Context) (*Placement, error) {
			return s.applications.GetPlacement(ctx, organizationID, applicationID, targetID)
		},
		authorize: func(ctx context.Context, p *Placement) (authorization.Decision, error) {
			return s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionPlacementConfigure, organizationID, applicationID, config)
		},
		apply: func(ctx context.Context, p *Placement) error {
			if _, err := s.activeApplication(ctx, organizationID, applicationID); err != nil {
				return err
			}
			if config == nil {
				if p.ApplyConfig() == nil {
					return errUnchanged
				}
				p.ClearApplyConfig()
				return nil
			}
			if err := s.checkDefinitionArtifact(ctx, organizationID, applicationID, config.DefinitionArtifact); err != nil {
				return err
			}
			return p.Configure(*config)
		},
		save:     s.applications.UpdatePlacement,
		snapshot: func(p *Placement) any { return p.Snapshot() },
		event: func(*Placement) audit.Event {
			return audit.Event{Type: EventPlacementUpdated, OrganizationID: organizationID, ResourceID: applicationID}
		},
	})
}

// RemovePlacement removes an Application from a Target. It is refused once
// Deployments refer to the placement, because their history needs it.
func (s *service) RemovePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string) error {
	d, err := s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionPlacementRemove, organizationID, applicationID, targetID)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.applications.GetPlacement(ctx, organizationID, applicationID, targetID)
		if err != nil {
			return err
		}
		p.Remove()
		if err := s.applications.DeletePlacement(ctx, organizationID, applicationID, targetID); err != nil {
			return fmt.Errorf("delete placement: %w", err)
		}
		if err := s.recordEvents(ctx, AggregatePlacement, p.Key(), p); err != nil {
			return err
		}
		return s.record(ctx, d, audit.Event{Type: EventPlacementRemoved, OrganizationID: organizationID, ResourceID: applicationID, Actor: actor, Before: p.Snapshot()})
	})
}
