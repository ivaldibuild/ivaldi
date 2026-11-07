package orchestration

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// AgentConfig is how an Agent runs: its system prompt today, and later any
// harness, model, or tool configuration. It is keyed by the IAM Agent it
// configures; identity stays in IAM (ADR 0007).
type AgentConfig struct {
	aggregate.Events
	organizationID string
	agentID        string
	systemPrompt   string
	createdAt      time.Time
	updatedAt      time.Time
}

// AgentConfigSnapshot is an AgentConfig's state, for repositories, audit
// records, and events. Its JSON form is part of the AgentConfig events'
// contract.
type AgentConfigSnapshot struct {
	OrganizationID string    `json:"organization_id"`
	AgentID        string    `json:"agent_id"`
	SystemPrompt   string    `json:"system_prompt"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// NewAgentConfigProps describes an AgentConfig. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewAgentConfigProps struct {
	OrganizationID string
	AgentID        string
	SystemPrompt   string
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

// NewAgentConfig returns an AgentConfig.
func NewAgentConfig(props NewAgentConfigProps) (*AgentConfig, error) {
	if !id.Valid(iam.Organization{}.Prefix(), props.OrganizationID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not an organization ID", props.OrganizationID))
	}
	if !id.Valid(iam.Agent{}.Prefix(), props.AgentID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a principal ID", props.AgentID))
	}
	if err := validateSystemPrompt(props.SystemPrompt); err != nil {
		return nil, err
	}
	now := clock.Now()
	c := &AgentConfig{
		organizationID: props.OrganizationID,
		agentID:        props.AgentID,
		systemPrompt:   props.SystemPrompt,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	// An AgentConfig has no ID of its own; a stored one has a creation time.
	if props.CreatedAt == nil {
		c.RecordEvent(AgentConfigCreated{c.Snapshot()})
	}
	return c, nil
}

func validateSystemPrompt(prompt string) error {
	if len(prompt) > maxSystemPromptLength {
		return NewErrInvalidInput(fmt.Sprintf("system prompt must be at most %d bytes", maxSystemPromptLength))
	}
	return nil
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}

func (c *AgentConfig) OrganizationID() string { return c.organizationID }
func (c *AgentConfig) AgentID() string        { return c.agentID }
func (c *AgentConfig) SystemPrompt() string   { return c.systemPrompt }
func (c *AgentConfig) CreatedAt() time.Time   { return c.createdAt }
func (c *AgentConfig) UpdatedAt() time.Time   { return c.updatedAt }

// SetSystemPrompt replaces the Agent's instructions; "" clears them.
func (c *AgentConfig) SetSystemPrompt(prompt string) error {
	if err := validateSystemPrompt(prompt); err != nil {
		return err
	}
	changed := prompt != c.systemPrompt
	c.systemPrompt = prompt
	c.updatedAt = clock.Now()
	if changed {
		c.RecordEvent(AgentConfigUpdated{c.Snapshot()})
	}
	return nil
}

func (c *AgentConfig) Snapshot() AgentConfigSnapshot {
	return AgentConfigSnapshot{
		OrganizationID: c.organizationID,
		AgentID:        c.agentID,
		SystemPrompt:   c.systemPrompt,
		CreatedAt:      c.createdAt,
		UpdatedAt:      c.updatedAt,
	}
}
