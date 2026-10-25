package iam

// Events an AgentSession records. Each carries the AgentSession as it is
// after the change. Changes common to every principal, such as suspension,
// are recorded as principal events.

// AgentSessionCreated is recorded when an orchestrated Session gets its
// identity.
type AgentSessionCreated struct {
	AgentSession AgentSessionSnapshot `json:"agent_session"`
}

// AgentSessionRevoked is recorded when an AgentSession's access ends for
// good.
type AgentSessionRevoked struct {
	AgentSession AgentSessionSnapshot `json:"agent_session"`
}

func (AgentSessionCreated) EventName() string { return "iam.v1.agent_session.created" }
func (AgentSessionRevoked) EventName() string { return "iam.v1.agent_session.revoked" }
