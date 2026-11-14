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
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		EnvironmentID:  s.EnvironmentID,
		ClusterID:      s.ClusterID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Runtime:        string(s.Runtime),
		Locator:        locator,
		CanObserve:     s.CanObserve,
		CanExecute:     s.CanExecute,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		DeletedAt:      s.DeletedAt,
	}, nil
}

func (m *targetMapper) ToDomain(e *targetEntity) (*delivery.Target, error) {
	var locator map[string]string
	if err := json.Unmarshal(e.Locator, &locator); err != nil {
		return nil, fmt.Errorf("decode locator of target %s: %w", e.ID, err)
	}
	return delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: e.OrganizationID,
		EnvironmentID:  e.EnvironmentID,
		ClusterID:      e.ClusterID,
		Slug:           e.Slug,
		DisplayName:    e.DisplayName,
		Runtime:        delivery.Runtime(e.Runtime),
		Locator:        locator,
		CanObserve:     e.CanObserve,
		CanExecute:     e.CanExecute,
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
		DeletedAt:      utcPtr(e.DeletedAt),
	}, &e.ID)
}
