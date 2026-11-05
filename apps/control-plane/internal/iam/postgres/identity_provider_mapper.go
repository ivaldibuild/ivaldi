package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// managedByConfiguration marks a provider the control plane's configuration
// manages. The table has no column for it, so it is kept in the config JSON.
const managedByConfiguration = "configuration"

// storedProviderConfig is the config column: the provider's settings and
// who manages it.
type storedProviderConfig struct {
	iam.IdentityProviderConfig
	ManagedBy string `json:"managed_by,omitempty"`
}

type identityProviderMapper struct{}

func newIdentityProviderMapper() *identityProviderMapper {
	return &identityProviderMapper{}
}

func (m *identityProviderMapper) ToPersistence(p *iam.IdentityProvider) (*identityProviderEntity, error) {
	s := p.Snapshot()
	stored := storedProviderConfig{IdentityProviderConfig: s.Config}
	if s.ManagedByConfiguration {
		stored.ManagedBy = managedByConfiguration
	}
	config, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("encode identity provider config: %w", err)
	}
	return &identityProviderEntity{
		ID:              s.ID,
		OrganizationID:  s.OrganizationID,
		Protocol:        string(s.Protocol),
		Name:            s.Name,
		Config:          config,
		SecretRef:       s.SecretRef,
		JITProvisioning: s.JITProvisioning,
		Status:          string(s.Status),
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}, nil
}

func (m *identityProviderMapper) ToDomain(e *identityProviderEntity) (*iam.IdentityProvider, error) {
	var stored storedProviderConfig
	if err := json.Unmarshal(e.Config, &stored); err != nil {
		return nil, fmt.Errorf("decode identity provider config: %w", err)
	}
	return iam.NewIdentityProvider(iam.NewIdentityProviderProps{
		OrganizationID:         e.OrganizationID,
		Protocol:               iam.IdentityProtocol(e.Protocol),
		Name:                   e.Name,
		Config:                 stored.IdentityProviderConfig,
		ManagedByConfiguration: stored.ManagedBy == managedByConfiguration,
		SecretRef:              e.SecretRef,
		JITProvisioning:        e.JITProvisioning,
		Status:                 iam.IdentityProviderStatus(e.Status),
		CreatedAt:              utc(e.CreatedAt),
		UpdatedAt:              utc(e.UpdatedAt),
	}, &e.ID)
}
