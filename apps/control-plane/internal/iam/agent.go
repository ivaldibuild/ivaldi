package iam

// Agent is a durable agent identity in an Organization, such as
// release-agent or triage-agent (ADR 0007). It can belong to Teams and hold
// grants, and every agent Session will run as one. IAM holds only the
// identity: how an Agent runs (its harness, model, instructions, and tools)
// belongs to orchestration.
//
// Agents are created through a Team, which is accountable for them: the
// team capability keeps every Agent in at least one active Team.
type Agent struct {
	principal
	slug string
}

// AgentSnapshot is an Agent's state, for repositories, audit records, and
// events. Its JSON form is part of the Agent events' contract.
type AgentSnapshot struct {
	PrincipalSnapshot
	Slug string `json:"slug"`
}

// NewAgentProps describes an Agent. Its display name is the principal's.
type NewAgentProps struct {
	PrincipalProps
	// Slug names the Agent, unique among the Organization's Agents.
	Slug string
}

// NewAgent returns an Agent, with a new ID unless principalID gives a stored
// one.
func NewAgent(props NewAgentProps, principalID *string) (*Agent, error) {
	p, err := newPrincipal(PrincipalAgent, props.PrincipalProps, principalID)
	if err != nil {
		return nil, err
	}
	if err := validateSlug(props.Slug); err != nil {
		return nil, err
	}
	a := &Agent{principal: p, slug: props.Slug}
	if a.IsNew() {
		a.RecordEvent(AgentCreated{a.Snapshot()})
	}
	return a, nil
}

func (a *Agent) Slug() string { return a.slug }

func (a *Agent) Snapshot() AgentSnapshot {
	return AgentSnapshot{PrincipalSnapshot: a.snapshot(), Slug: a.slug}
}
