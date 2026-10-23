package delivery

// Events an Application records. Each carries the Application as it is after
// the change; the audit record keeps the state before it.

// ApplicationCreated is recorded when an Application is created.
type ApplicationCreated struct {
	Application ApplicationSnapshot `json:"application"`
}

// ApplicationUpdated is recorded when an Application's slug, display name,
// or workload selector changes.
type ApplicationUpdated struct {
	Application ApplicationSnapshot `json:"application"`
}

// ApplicationSourceLinked is recorded when an Application is linked to the
// Source it is built from, or to a different one.
type ApplicationSourceLinked struct {
	Application ApplicationSnapshot `json:"application"`
}

// ApplicationSourceUnlinked is recorded when an Application's Source is removed.
type ApplicationSourceUnlinked struct {
	Application ApplicationSnapshot `json:"application"`
}

// ApplicationTransferred is recorded when an Application moves to another
// Team. The snapshot holds the new owner.
type ApplicationTransferred struct {
	Application         ApplicationSnapshot `json:"application"`
	PreviousOwnerTeamID string              `json:"previous_owner_team_id"`
}

// ApplicationArchived is recorded when an Application is archived.
type ApplicationArchived struct {
	Application ApplicationSnapshot `json:"application"`
}

// ApplicationRestored is recorded when an archived Application is restored.
type ApplicationRestored struct {
	Application ApplicationSnapshot `json:"application"`
}

func (ApplicationCreated) EventName() string        { return "delivery.v1.application.created" }
func (ApplicationUpdated) EventName() string        { return "delivery.v1.application.updated" }
func (ApplicationSourceLinked) EventName() string   { return "delivery.v1.application.source_linked" }
func (ApplicationSourceUnlinked) EventName() string { return "delivery.v1.application.source_unlinked" }
func (ApplicationTransferred) EventName() string    { return "delivery.v1.application.transferred" }
func (ApplicationArchived) EventName() string       { return "delivery.v1.application.archived" }
func (ApplicationRestored) EventName() string       { return "delivery.v1.application.restored" }
