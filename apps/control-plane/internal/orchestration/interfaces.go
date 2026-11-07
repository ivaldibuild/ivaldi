package orchestration

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// ListAgentsQuery lists an Organization's Agents. It sorts by display_name
// (the default), slug, or created_at, with the ID breaking ties.
type ListAgentsQuery struct {
	OrganizationID  string
	Search          string
	IncludeInactive bool
	Page            pagination.Query
}

// CreateAgentInput describes an Agent, its first Team, and its
// configuration. SystemPrompt may be empty.
type CreateAgentInput struct {
	OrganizationID string
	TeamID         string
	Slug           string
	DisplayName    string
	SystemPrompt   string
	Role           team.Role
}

// UpdateAgentProps changes the fields that are set. An empty SystemPrompt
// clears the prompt.
type UpdateAgentProps struct {
	DisplayName  *string
	SystemPrompt *string
}

// AgentSummary is an Agent as a list shows it. The prompt itself is omitted
// because it can be large; HasSystemPrompt says whether one is set.
type AgentSummary struct {
	ID              string              `json:"id"`
	OrganizationID  string              `json:"organization_id"`
	Kind            iam.PrincipalKind   `json:"kind"`
	DisplayName     string              `json:"display_name"`
	Status          iam.PrincipalStatus `json:"status"`
	Metadata        map[string]string   `json:"metadata"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	Slug            string              `json:"slug"`
	Teams           []team.Membership   `json:"teams"`
	HasSystemPrompt bool                `json:"has_system_prompt"`
}

// AgentDetail is one Agent with its configuration and Teams.
type AgentDetail struct {
	ID             string              `json:"id"`
	OrganizationID string              `json:"organization_id"`
	Kind           iam.PrincipalKind   `json:"kind"`
	DisplayName    string              `json:"display_name"`
	Status         iam.PrincipalStatus `json:"status"`
	Metadata       map[string]string   `json:"metadata"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Slug           string              `json:"slug"`
	SystemPrompt   string              `json:"system_prompt"`
	Teams          []team.Membership   `json:"teams"`
}

// AgentConfigRepository persists and retrieves Agent configuration.
type AgentConfigRepository interface {
	Get(ctx context.Context, organizationID, agentID string) (*AgentConfig, error)
	// ListByAgentIDs returns the configurations that exist, keyed by Agent ID.
	ListByAgentIDs(ctx context.Context, organizationID string, agentIDs []string) (map[string]*AgentConfig, error)
	// Upsert inserts or updates the configuration.
	Upsert(ctx context.Context, config *AgentConfig) error
}

// AgentService manages an Organization's Agents and their configuration.
type AgentService interface {
	CreateAgent(ctx context.Context, actor identity.Actor, in CreateAgentInput) (*AgentDetail, error)
	GetAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*AgentDetail, error)
	ListAgents(ctx context.Context, actor identity.Actor, query ListAgentsQuery) (*pagination.Result[AgentSummary], error)
	UpdateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string, props UpdateAgentProps) (*AgentDetail, error)
	SuspendAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*AgentDetail, error)
	ReactivateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*AgentDetail, error)
}
