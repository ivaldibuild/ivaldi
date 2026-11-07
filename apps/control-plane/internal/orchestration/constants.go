package orchestration

// Limits on what one record may hold.
const (
	// maxSystemPromptLength leaves room for substantial instructions while
	// keeping one API request and one audit record bounded.
	maxSystemPromptLength = 64 * 1024
)

// AggregateAgentConfig names the agent configuration object that raises
// events. Its key is the Agent's ID.
const AggregateAgentConfig = "orchestration.AgentConfig"

// Actions name what an actor asks the authorizer to allow.
const (
	ActionAgentCreate     = "agent.create"
	ActionAgentList       = "agent.list"
	ActionAgentRead       = "agent.read"
	ActionAgentUpdate     = "agent.update"
	ActionAgentSuspend    = "agent.suspend"
	ActionAgentReactivate = "agent.reactivate"
)

// Audit event types this capability emits. Identity changes, such as a
// rename or status change, are recorded by IAM's events instead.
const (
	EventAgentConfigCreated = "agent_config.created"
	EventAgentConfigUpdated = "agent_config.updated"
)
