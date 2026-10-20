package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// applicationMapper maps Applications and what belongs to them: artifacts,
// Channels with their rules, and placements.
type applicationMapper struct{}

func newApplicationMapper() *applicationMapper {
	return &applicationMapper{}
}

func (m *applicationMapper) ToPersistence(a *delivery.Application) (*applicationEntity, error) {
	s := a.Snapshot()
	selector, err := json.Marshal(s.WorkloadSelector)
	if err != nil {
		return nil, fmt.Errorf("encode workload selector: %w", err)
	}
	return &applicationEntity{
		ID:               s.ID,
		OrganizationID:   s.OrganizationID,
		WorkspaceID:      s.WorkspaceID,
		ProjectID:        s.ProjectID,
		OwnerTeamID:      s.OwnerTeamID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		SourceID:         s.SourceID,
		WorkloadSelector: selector,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		ArchivedAt:       s.ArchivedAt,
	}, nil
}

func (m *applicationMapper) ToDomain(e *applicationEntity) (*delivery.Application, error) {
	var selector map[string]string
	if len(e.WorkloadSelector) > 0 {
		if err := json.Unmarshal(e.WorkloadSelector, &selector); err != nil {
			return nil, fmt.Errorf("decode workload selector of application %s: %w", e.ID, err)
		}
	}
	return delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID:   e.OrganizationID,
		WorkspaceID:      e.WorkspaceID,
		ProjectID:        e.ProjectID,
		OwnerTeamID:      e.OwnerTeamID,
		Slug:             e.Slug,
		DisplayName:      e.DisplayName,
		SourceID:         e.SourceID,
		WorkloadSelector: selector,
		CreatedAt:        utc(e.CreatedAt),
		UpdatedAt:        utc(e.UpdatedAt),
		ArchivedAt:       utcPtr(e.ArchivedAt),
	}, &e.ID)
}

func (m *applicationMapper) ArtifactToPersistence(a *delivery.Artifact) *artifactEntity {
	s := a.Snapshot()
	return &artifactEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		ApplicationID:  s.ApplicationID,
		Name:           s.Name,
		ArtifactKind:   string(s.ArtifactKind),
		ArtifactName:   s.ArtifactName,
		RegistryID:     s.RegistryID,
		Required:       s.Required,
		Source:         string(s.Source),
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}
}

func (m *applicationMapper) ArtifactToDomain(e *artifactEntity) (*delivery.Artifact, error) {
	return delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: e.OrganizationID,
		ApplicationID:  e.ApplicationID,
		Name:           e.Name,
		ArtifactKind:   delivery.ArtifactKind(e.ArtifactKind),
		ArtifactName:   e.ArtifactName,
		RegistryID:     e.RegistryID,
		Required:       e.Required,
		Source:         delivery.ArtifactSource(e.Source),
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
		ArchivedAt:     utcPtr(e.ArchivedAt),
	}, &e.ID)
}

// channelRows are the rows of one Channel.
type channelRows struct {
	channel *channelEntity
	rules   []channelRuleEntity
}

func (m *applicationMapper) ChannelToPersistence(c *delivery.Channel) channelRows {
	s := c.Snapshot()
	rows := channelRows{channel: &channelEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		ApplicationID:  s.ApplicationID,
		LifecycleID:    s.LifecycleID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Description:    s.Description,
		IsDefault:      s.IsDefault,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}}
	for _, r := range s.Rules {
		rows.rules = append(rows.rules, channelRuleEntity{
			ID:             r.ID,
			OrganizationID: s.OrganizationID,
			ApplicationID:  s.ApplicationID,
			ChannelID:      s.ID,
			Kind:           string(r.Kind),
			ArtifactID:     r.ArtifactID,
			Config:         r.Config,
			CreatedAt:      r.CreatedAt,
			UpdatedAt:      r.UpdatedAt,
		})
	}
	return rows
}

// ChannelToDomain builds a Channel from its row and its rules in the order
// they were created.
func (m *applicationMapper) ChannelToDomain(rows channelRows) (*delivery.Channel, error) {
	rules := make([]delivery.NewChannelRuleProps, len(rows.rules))
	for i, r := range rows.rules {
		rules[i] = delivery.NewChannelRuleProps{
			ID:         &r.ID,
			Kind:       release.RuleKind(r.Kind),
			ArtifactID: r.ArtifactID,
			Config:     r.Config,
			CreatedAt:  utc(r.CreatedAt),
			UpdatedAt:  utc(r.UpdatedAt),
		}
	}
	c := rows.channel
	return delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: c.OrganizationID,
		ApplicationID:  c.ApplicationID,
		LifecycleID:    c.LifecycleID,
		Slug:           c.Slug,
		DisplayName:    c.DisplayName,
		Description:    c.Description,
		IsDefault:      c.IsDefault,
		Rules:          rules,
		CreatedAt:      utc(c.CreatedAt),
		UpdatedAt:      utc(c.UpdatedAt),
		ArchivedAt:     utcPtr(c.ArchivedAt),
	}, &c.ID)
}

func (m *applicationMapper) PlacementToPersistence(p *delivery.Placement) (*placementEntity, error) {
	s := p.Snapshot()
	var config []byte
	if s.ApplyConfig != nil {
		var err error
		if config, err = json.Marshal(s.ApplyConfig); err != nil {
			return nil, fmt.Errorf("encode apply config: %w", err)
		}
	}
	return &placementEntity{
		OrganizationID:      s.OrganizationID,
		ApplicationID:       s.ApplicationID,
		TargetID:            s.TargetID,
		Source:              string(s.Source),
		ApplyConfig:         config,
		CurrentDeploymentID: s.CurrentDeploymentID,
		CurrentReleaseID:    s.CurrentReleaseID,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}, nil
}

func (m *applicationMapper) PlacementToDomain(e *placementEntity) (*delivery.Placement, error) {
	var config *delivery.ApplyConfig
	if len(e.ApplyConfig) > 0 {
		config = &delivery.ApplyConfig{}
		if err := json.Unmarshal(e.ApplyConfig, config); err != nil {
			return nil, fmt.Errorf("decode apply config of application %s on target %s: %w", e.ApplicationID, e.TargetID, err)
		}
	}
	return delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID:      e.OrganizationID,
		ApplicationID:       e.ApplicationID,
		TargetID:            e.TargetID,
		Source:              delivery.PlacementSource(e.Source),
		ApplyConfig:         config,
		CurrentDeploymentID: e.CurrentDeploymentID,
		CurrentReleaseID:    e.CurrentReleaseID,
		CreatedAt:           utc(e.CreatedAt),
		UpdatedAt:           utc(e.UpdatedAt),
	})
}
