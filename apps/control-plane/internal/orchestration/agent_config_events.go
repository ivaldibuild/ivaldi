package orchestration

// Events an AgentConfig records. Each carries the configuration after the
// change.

// AgentConfigCreated is recorded when an Agent gets its configuration.
type AgentConfigCreated struct {
	AgentConfig AgentConfigSnapshot `json:"agent_config"`
}

// AgentConfigUpdated is recorded when an Agent's configuration changes.
type AgentConfigUpdated struct {
	AgentConfig AgentConfigSnapshot `json:"agent_config"`
}

func (AgentConfigCreated) EventName() string { return "orchestration.v1.agent_config.created" }
func (AgentConfigUpdated) EventName() string { return "orchestration.v1.agent_config.updated" }
