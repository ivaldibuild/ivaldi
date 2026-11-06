package iam

// Events an Agent records. Each carries the Agent as it is after the change.
// Changes common to every principal, such as its status, are recorded as
// principal events.

// AgentCreated is recorded when an Agent is created.
type AgentCreated struct {
	Agent AgentSnapshot `json:"agent"`
}

func (AgentCreated) EventName() string { return "iam.v1.agent.created" }
