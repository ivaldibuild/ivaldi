package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type targetMapper struct{}

func newTargetMapper() *targetMapper {
	return &targetMapper{}
}

func (m *targetMapper) ToPersistence(t *delivery.Target) (*targetEntity, error) {
	s := t.Snapshot()
	locator, err := json.Marshal(s.Locator)
	if err != nil {
		return nil, fmt.Errorf("encode locator: %w", err)
	}
	return &targetEntity{
		ID:               s.ID,
		OrganizationID:   s.OrganizationID,
		EnvironmentID:    s.EnvironmentID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		Runtime:          string(s.Runtime),
		Locator:          locator,
		AgentPrincipalID: s.AgentPrincipalID,
		LastReportedAt:   s.LastReportedAt,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		DeletedAt:        s.DeletedAt,
	}, nil
}

func (m *targetMapper) ToDomain(e *targetEntity) (*delivery.Target, error) {
	var locator map[string]string
	if err := json.Unmarshal(e.Locator, &locator); err != nil {
		return nil, fmt.Errorf("decode locator of target %s: %w", e.ID, err)
	}
	return delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID:   e.OrganizationID,
		EnvironmentID:    e.EnvironmentID,
		Slug:             e.Slug,
		DisplayName:      e.DisplayName,
		Runtime:          delivery.Runtime(e.Runtime),
		Locator:          locator,
		AgentPrincipalID: e.AgentPrincipalID,
		LastReportedAt:   utcPtr(e.LastReportedAt),
		CreatedAt:        utc(e.CreatedAt),
		UpdatedAt:        utc(e.UpdatedAt),
		DeletedAt:        utcPtr(e.DeletedAt),
	}, &e.ID)
}
