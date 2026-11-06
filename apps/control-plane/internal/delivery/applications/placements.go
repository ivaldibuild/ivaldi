package applications

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CreatePlacement places an active Application on an active Target in its
// Workspace. Its audit record names the Application, since a placement has
// no ID of its own.
func (s *service) CreatePlacement(ctx context.Context, actor identity.Actor, in delivery.CreatePlacementInput) (*delivery.Placement, error) {
	p, err := delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: in.OrganizationID,
		ApplicationID:  in.ApplicationID,
		TargetID:       in.TargetID,
		Source:         delivery.PlacementConfigured,
		ApplyConfig:    in.ApplyConfig,
	})
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.activeApplication(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		t, err := s.targets.GetTarget(ctx, in.OrganizationID, in.TargetID)
		if err != nil {
			return err
		}
		if t.Archived() {
			return fmt.Errorf("%w: target %s", delivery.ErrArchived, t.ID())
		}
		e, err := s.environments.GetEnvironment(ctx, in.OrganizationID, t.EnvironmentID())
		if err != nil {
			return err
		}
		if e.WorkspaceID() != app.WorkspaceID() {
			return fmt.Errorf("%w: target %s is not in workspace %s", delivery.ErrWorkspaceMismatch, t.ID(), app.WorkspaceID())
		}
		if e.Archived() {
			return fmt.Errorf("%w: environment %s", delivery.ErrArchived, e.ID())
		}
		if c := p.ApplyConfig(); c != nil {
			if err := s.checkDefinitionArtifact(ctx, in.OrganizationID, in.ApplicationID, c.DefinitionArtifact); err != nil {
				return err
			}
		}
		if err := s.applications.CreatePlacement(ctx, p); err != nil {
			return fmt.Errorf("create placement: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregatePlacement, p.Key(), p); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventPlacementCreated, OrganizationID: in.OrganizationID, ResourceID: in.ApplicationID, Actor: actor, After: p.Snapshot()})
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
	return fmt.Errorf("%w: application %s has no active artifact named %q", delivery.ErrNotFound, applicationID, name)
}

func (s *service) ListPlacements(ctx context.Context, _ identity.Actor, organizationID, applicationID string) ([]*delivery.Placement, error) {
	return s.applications.ListPlacements(ctx, organizationID, applicationID)
}

// ConfigurePlacement sets how Releases are applied to the Target, or removes
// it when config is nil so Deployments there are tracked only.
func (s *service) ConfigurePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string, config *delivery.ApplyConfig) (*delivery.Placement, error) {
	if config != nil {
		if err := config.Validate(); err != nil {
			return nil, err
		}
	}
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Placement]{
		AggregateType: delivery.AggregatePlacement,
		AggregateID:   (*delivery.Placement).Key,
		Load: func(ctx context.Context) (*delivery.Placement, error) {
			return s.applications.GetPlacement(ctx, organizationID, applicationID, targetID)
		},
		Apply: func(ctx context.Context, p *delivery.Placement) error {
			if _, err := s.activeApplication(ctx, organizationID, applicationID); err != nil {
				return err
			}
			if config == nil {
				if p.ApplyConfig() == nil {
					return change.ErrUnchanged
				}
				p.ClearApplyConfig()
				return nil
			}
			if err := s.checkDefinitionArtifact(ctx, organizationID, applicationID, config.DefinitionArtifact); err != nil {
				return err
			}
			return p.Configure(*config)
		},
		Save:     s.applications.UpdatePlacement,
		Snapshot: func(p *delivery.Placement) any { return p.Snapshot() },
		Event: func(*delivery.Placement) audit.Event {
			return audit.Event{Type: delivery.EventPlacementUpdated, OrganizationID: organizationID, ResourceID: applicationID}
		},
	})
}

// RemovePlacement removes an Application from a Target. It is refused once
// Deployments refer to the placement, because their history needs it.
func (s *service) RemovePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string) error {
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.applications.GetPlacement(ctx, organizationID, applicationID, targetID)
		if err != nil {
			return err
		}
		p.Remove()
		if err := s.applications.DeletePlacement(ctx, organizationID, applicationID, targetID); err != nil {
			return fmt.Errorf("delete placement: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregatePlacement, p.Key(), p); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventPlacementRemoved, OrganizationID: organizationID, ResourceID: applicationID, Actor: actor, Before: p.Snapshot()})
	})
}
