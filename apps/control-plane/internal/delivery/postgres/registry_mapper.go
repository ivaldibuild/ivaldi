package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type registryMapper struct{}

func newRegistryMapper() *registryMapper {
	return &registryMapper{}
}

func (m *registryMapper) ToPersistence(r *delivery.Registry) *registryEntity {
	s := r.Snapshot()
	return &registryEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		WorkspaceID:    s.WorkspaceID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Kind:           string(s.Kind),
		URL:            s.URL,
		CredentialRef:  s.CredentialRef,
		LookupFrom:     string(s.LookupFrom),
		LookupPool:     s.LookupPool,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		DeletedAt:      s.DeletedAt,
	}
}

func (m *registryMapper) ToDomain(e *registryEntity) (*delivery.Registry, error) {
	return delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: e.OrganizationID,
		WorkspaceID:    e.WorkspaceID,
		Slug:           e.Slug,
		DisplayName:    e.DisplayName,
		Kind:           delivery.RegistryKind(e.Kind),
		URL:            e.URL,
		CredentialRef:  e.CredentialRef,
		LookupFrom:     delivery.LookupFrom(e.LookupFrom),
		LookupPool:     e.LookupPool,
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
		DeletedAt:      utcPtr(e.DeletedAt),
	}, &e.ID)
}
