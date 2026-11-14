package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type clusterMapper struct{}

func newClusterMapper() *clusterMapper {
	return &clusterMapper{}
}

func (m *clusterMapper) ToPersistence(c *delivery.Cluster) (*clusterEntity, error) {
	s := c.Snapshot()
	capabilities, err := json.Marshal(s.AgentCapabilities)
	if err != nil {
		return nil, fmt.Errorf("encode agent capabilities: %w", err)
	}
	var failure []byte
	if s.AgentLastFailure != nil {
		failure, err = json.Marshal(s.AgentLastFailure)
		if err != nil {
			return nil, fmt.Errorf("encode agent failure: %w", err)
		}
	}
	return &clusterEntity{
		ID:                s.ID,
		OrganizationID:    s.OrganizationID,
		Slug:              s.Slug,
		DisplayName:       s.DisplayName,
		Runtime:           string(s.Runtime),
		AgentPrincipalID:  s.AgentPrincipalID,
		AgentCapabilities: capabilities,
		AgentLastFailure:  failure,
		LastReportedAt:    s.LastReportedAt,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		DeletedAt:         s.DeletedAt,
	}, nil
}

func (m *clusterMapper) ToDomain(e *clusterEntity) (*delivery.Cluster, error) {
	var capabilities map[string]any
	if len(e.AgentCapabilities) > 0 {
		if err := json.Unmarshal(e.AgentCapabilities, &capabilities); err != nil {
			return nil, fmt.Errorf("decode agent capabilities of cluster %s: %w", e.ID, err)
		}
	}
	var failure *delivery.AgentFailure
	if len(e.AgentLastFailure) > 0 {
		failure = &delivery.AgentFailure{}
		if err := json.Unmarshal(e.AgentLastFailure, failure); err != nil {
			return nil, fmt.Errorf("decode agent failure of cluster %s: %w", e.ID, err)
		}
	}
	return delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID:    e.OrganizationID,
		Slug:              e.Slug,
		DisplayName:       e.DisplayName,
		Runtime:           delivery.Runtime(e.Runtime),
		AgentPrincipalID:  e.AgentPrincipalID,
		AgentCapabilities: capabilities,
		AgentLastFailure:  failure,
		LastReportedAt:    utcPtr(e.LastReportedAt),
		CreatedAt:         utc(e.CreatedAt),
		UpdatedAt:         utc(e.UpdatedAt),
		DeletedAt:         utcPtr(e.DeletedAt),
	}, &e.ID)
}
