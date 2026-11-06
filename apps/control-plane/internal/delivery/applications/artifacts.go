package applications

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CreateArtifact declares an artifact for an active Application.
func (s *service) CreateArtifact(ctx context.Context, actor identity.Actor, in delivery.CreateArtifactInput) (*delivery.Artifact, error) {
	a, err := delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: in.OrganizationID,
		ApplicationID:  in.ApplicationID,
		Name:           in.Name,
		ArtifactKind:   in.ArtifactKind,
		ArtifactName:   in.ArtifactName,
		RegistryID:     &in.RegistryID,
		Required:       in.Required,
		Source:         delivery.ArtifactDeclared,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
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
		if err := s.w.Events(ctx, delivery.AggregateArtifact, a.ID(), a); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventArtifactCreated, OrganizationID: in.OrganizationID, ResourceID: a.ID(), Actor: actor, After: a.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *service) ListArtifacts(ctx context.Context, _ identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error) {
	return s.applications.ListArtifacts(ctx, organizationID, applicationID, includeArchived)
}

func (s *service) changeArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID, event string, apply func(context.Context, *delivery.Artifact) error) (*delivery.Artifact, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Artifact]{
		AggregateType: delivery.AggregateArtifact,
		AggregateID:   func(a *delivery.Artifact) string { return a.ID() },
		Load: func(ctx context.Context) (*delivery.Artifact, error) {
			return s.applications.GetArtifact(ctx, organizationID, artifactID)
		},
		// An archived Application's artifacts change only once it is restored.
		Apply: func(ctx context.Context, a *delivery.Artifact) error {
			if _, err := s.activeApplication(ctx, organizationID, a.ApplicationID()); err != nil {
				return err
			}
			return apply(ctx, a)
		},
		Save:     s.applications.UpdateArtifact,
		Snapshot: func(a *delivery.Artifact) any { return a.Snapshot() },
		Event: func(a *delivery.Artifact) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
}

// UpdateArtifact changes an active artifact. Renaming one that a placement
// applies Releases from is refused, because the placement refers to it by name.
func (s *service) UpdateArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string, props delivery.UpdateArtifactProps) (*delivery.Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, delivery.EventArtifactUpdated,
		func(ctx context.Context, a *delivery.Artifact) error {
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
func (s *service) SetArtifactRegistry(ctx context.Context, actor identity.Actor, organizationID, artifactID, registryID string) (*delivery.Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, delivery.EventArtifactUpdated,
		func(ctx context.Context, a *delivery.Artifact) error {
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
func (s *service) DeclareArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, delivery.EventArtifactUpdated,
		func(_ context.Context, a *delivery.Artifact) error { return a.Declare() })
}

// ArchiveArtifact archives an artifact that no active Channel's rules are
// scoped to and no placement applies Releases from. Archiving an archived
// one does nothing.
func (s *service) ArchiveArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, delivery.EventArtifactArchived,
		func(ctx context.Context, a *delivery.Artifact) error {
			if a.Archived() {
				return change.ErrUnchanged
			}
			channels, err := s.applications.ListChannels(ctx, organizationID, a.ApplicationID(), false)
			if err != nil {
				return fmt.Errorf("list channels: %w", err)
			}
			for _, c := range channels {
				for _, r := range c.Rules() {
					if r.ArtifactID != nil && *r.ArtifactID == a.ID() {
						return fmt.Errorf("%w: rules of channel %s are scoped to artifact %s", delivery.ErrInUse, c.ID(), a.ID())
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
func (s *service) checkNotDefinition(ctx context.Context, a *delivery.Artifact) error {
	placements, err := s.applications.ListPlacements(ctx, a.OrganizationID(), a.ApplicationID())
	if err != nil {
		return fmt.Errorf("list placements: %w", err)
	}
	for _, p := range placements {
		if c := p.ApplyConfig(); c != nil && c.DefinitionArtifact == a.Name() {
			return fmt.Errorf("%w: target %s applies releases from artifact %q", delivery.ErrInUse, p.TargetID(), a.Name())
		}
	}
	return nil
}

// RestoreArtifact restores an archived artifact of an active Application.
func (s *service) RestoreArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	return s.changeArtifact(ctx, actor, organizationID, artifactID, delivery.EventArtifactRestored,
		func(_ context.Context, a *delivery.Artifact) error {
			if !a.Archived() {
				return change.ErrUnchanged
			}
			a.Restore()
			return nil
		})
}
