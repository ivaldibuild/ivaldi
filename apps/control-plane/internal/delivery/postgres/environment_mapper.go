package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type environmentMapper struct{}

func newEnvironmentMapper() *environmentMapper {
	return &environmentMapper{}
}

func (m *environmentMapper) ToPersistence(e *delivery.Environment) *environmentEntity {
	s := e.Snapshot()
	return &environmentEntity{
		ID:               s.ID,
		OrganizationID:   s.OrganizationID,
		WorkspaceID:      s.WorkspaceID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		Tier:             string(s.Tier),
		ExecutionEnabled: s.ExecutionEnabled,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		DeletedAt:        s.DeletedAt,
	}
}

func (m *environmentMapper) ToDomain(e *environmentEntity) (*delivery.Environment, error) {
	return delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID:   e.OrganizationID,
		WorkspaceID:      e.WorkspaceID,
		Slug:             e.Slug,
		DisplayName:      e.DisplayName,
		Tier:             delivery.Tier(e.Tier),
		ExecutionEnabled: e.ExecutionEnabled,
		CreatedAt:        utc(e.CreatedAt),
		UpdatedAt:        utc(e.UpdatedAt),
		DeletedAt:        utcPtr(e.DeletedAt),
	}, &e.ID)
}
