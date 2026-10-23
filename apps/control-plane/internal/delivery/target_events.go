package delivery

// Events a Target records. Each carries the Target as it is after the
// change; the audit record keeps the state before it. Agent reports record
// no event: they are contact, not changes to the Target.

// TargetCreated is recorded when a Target is created.
type TargetCreated struct {
	Target TargetSnapshot `json:"target"`
}

// TargetUpdated is recorded when a Target's slug, display name, or locator changes.
type TargetUpdated struct {
	Target TargetSnapshot `json:"target"`
}

// TargetAgentAssigned is recorded when a runtime agent is assigned to a
// Target, replacing PreviousAgentPrincipalID if there was one.
type TargetAgentAssigned struct {
	Target                   TargetSnapshot `json:"target"`
	PreviousAgentPrincipalID *string        `json:"previous_agent_principal_id"`
}

// TargetAgentUnassigned is recorded when a Target's runtime agent is removed.
type TargetAgentUnassigned struct {
	Target                   TargetSnapshot `json:"target"`
	PreviousAgentPrincipalID string         `json:"previous_agent_principal_id"`
}

// TargetArchived is recorded when a Target is archived.
type TargetArchived struct {
	Target TargetSnapshot `json:"target"`
}

// TargetRestored is recorded when an archived Target is restored.
type TargetRestored struct {
	Target TargetSnapshot `json:"target"`
}

func (TargetCreated) EventName() string         { return "delivery.v1.target.created" }
func (TargetUpdated) EventName() string         { return "delivery.v1.target.updated" }
func (TargetAgentAssigned) EventName() string   { return "delivery.v1.target.agent_assigned" }
func (TargetAgentUnassigned) EventName() string { return "delivery.v1.target.agent_unassigned" }
func (TargetArchived) EventName() string        { return "delivery.v1.target.archived" }
func (TargetRestored) EventName() string        { return "delivery.v1.target.restored" }
