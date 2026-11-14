package delivery

// Events a Target records. Each carries the Target as it is after the
// change; the audit record keeps the state before it. Agent reports and
// heartbeats record no event: they are contact, not changes to the Target.

// TargetCreated is recorded when a Target is created.
type TargetCreated struct {
	Target TargetSnapshot `json:"target"`
}

// TargetUpdated is recorded when a Target's slug, display name, or locator changes.
type TargetUpdated struct {
	Target TargetSnapshot `json:"target"`
}

// TargetArchived is recorded when a Target is archived.
type TargetArchived struct {
	Target TargetSnapshot `json:"target"`
}

// TargetRestored is recorded when an archived Target is restored.
type TargetRestored struct {
	Target TargetSnapshot `json:"target"`
}

func (TargetCreated) EventName() string  { return "delivery.v1.target.created" }
func (TargetUpdated) EventName() string  { return "delivery.v1.target.updated" }
func (TargetArchived) EventName() string { return "delivery.v1.target.archived" }
func (TargetRestored) EventName() string { return "delivery.v1.target.restored" }
