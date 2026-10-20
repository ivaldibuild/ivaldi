package delivery

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// CreateRegistryInput describes a new Registry.
type CreateRegistryInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug          string
	DisplayName   string
	Kind          RegistryKind
	URL           string
	CredentialRef string
	// LookupFrom defaults to LookupFromControlPlane.
	LookupFrom LookupFrom
	LookupPool string
}

// CreateRegistry creates a Registry in an active Workspace.
func (s *Service) CreateRegistry(ctx context.Context, actor identity.Actor, in CreateRegistryInput) (*Registry, error) {
	r, err := NewRegistry(NewRegistryProps{
		OrganizationID: in.OrganizationID,
		WorkspaceID:    in.WorkspaceID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Kind:           in.Kind,
		URL:            in.URL,
		CredentialRef:  &in.CredentialRef,
		LookupFrom:     in.LookupFrom,
		LookupPool:     &in.LookupPool,
	}, nil)
	if err != nil {
		return nil, err
	}
	d, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionRegistryManage, OrganizationID: in.OrganizationID,
		ResourceID: in.WorkspaceID, Action: ActionRegistryCreate, Proposal: in,
	})
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.activeWorkspace(ctx, r.OrganizationID(), r.WorkspaceID()); err != nil {
			return err
		}
		if err := s.registries.CreateRegistry(ctx, r); err != nil {
			return fmt.Errorf("create registry: %w", err)
		}
		return s.record(ctx, d, audit.Event{Type: EventRegistryCreated, OrganizationID: r.OrganizationID(), ResourceID: r.ID(), Actor: actor, After: r.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetRegistry returns a Registry if the actor may read its Workspace's Registries.
func (s *Service) GetRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error) {
	r, err := s.registries.GetRegistry(ctx, organizationID, registryID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionRegistryRead, OrganizationID: organizationID,
		ResourceID: r.WorkspaceID(), Action: ActionRegistryRead,
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// ListRegistries lists a Workspace's Registries.
func (s *Service) ListRegistries(ctx context.Context, actor identity.Actor, query ListRegistriesQuery) (*pagination.Result[*Registry], error) {
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionRegistryRead, OrganizationID: query.OrganizationID,
		ResourceID: query.WorkspaceID, Action: ActionRegistryList,
	}); err != nil {
		return nil, err
	}
	return s.registries.ListRegistries(ctx, query)
}

func (s *Service) changeRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID, action, event string, proposal any, apply func(context.Context, *Registry) error) (*Registry, error) {
	return run(ctx, s, actor, change[*Registry]{
		load: func(ctx context.Context) (*Registry, error) {
			return s.registries.GetRegistry(ctx, organizationID, registryID)
		},
		authorize: func(ctx context.Context, r *Registry) (authorization.Decision, error) {
			return s.authorize(ctx, authorization.Request{
				Actor: actor, Permission: PermissionRegistryManage, OrganizationID: organizationID,
				ResourceID: r.WorkspaceID(), Action: action, Proposal: proposal,
			})
		},
		apply:    apply,
		save:     s.registries.UpdateRegistry,
		snapshot: func(r *Registry) any { return r.Snapshot() },
		event: func(r *Registry) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: r.ID()}
		},
	})
}

// UpdateRegistry changes an active Registry's slug, name, or URL.
func (s *Service) UpdateRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string, props UpdateRegistryProps) (*Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, ActionRegistryUpdate, EventRegistryUpdated, props,
		func(_ context.Context, r *Registry) error { return r.Update(props) })
}

// SetRegistryCredential points the Registry at a credential, or makes it
// anonymous when credentialRef is empty.
func (s *Service) SetRegistryCredential(ctx context.Context, actor identity.Actor, organizationID, registryID, credentialRef string) (*Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, ActionRegistrySetCredential, EventRegistryUpdated, credentialRef,
		func(_ context.Context, r *Registry) error {
			if credentialRef == "" {
				return r.ClearCredential()
			}
			return r.SetCredential(credentialRef)
		})
}

// SetRegistryLookup changes where lookups against the Registry run.
func (s *Service) SetRegistryLookup(ctx context.Context, actor identity.Actor, organizationID, registryID string, from LookupFrom, pool string) (*Registry, error) {
	proposal := struct {
		LookupFrom LookupFrom
		LookupPool string
	}{from, pool}
	return s.changeRegistry(ctx, actor, organizationID, registryID, ActionRegistrySetLookup, EventRegistryUpdated, proposal,
		func(_ context.Context, r *Registry) error { return r.SetLookup(from, pool) })
}

// ArchiveRegistry archives a Registry. Artifacts that use it keep the
// reference, so their Builds can still be looked up once it is restored.
func (s *Service) ArchiveRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, ActionRegistryArchive, EventRegistryArchived, nil,
		func(_ context.Context, r *Registry) error {
			if r.Archived() {
				return errUnchanged
			}
			r.Archive()
			return nil
		})
}

// RestoreRegistry restores an archived Registry in an active Workspace.
func (s *Service) RestoreRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, ActionRegistryRestore, EventRegistryRestored, nil,
		func(ctx context.Context, r *Registry) error {
			if !r.Archived() {
				return errUnchanged
			}
			if err := s.activeWorkspace(ctx, organizationID, r.WorkspaceID()); err != nil {
				return err
			}
			r.Restore()
			return nil
		})
}
