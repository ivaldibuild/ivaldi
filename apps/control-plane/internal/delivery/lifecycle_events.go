package delivery

// Events a Lifecycle records. Each carries the Lifecycle, with its phases,
// as it is after the change; the audit record keeps the state before it.
// Phase events also name the phase.

// LifecycleCreated is recorded when a Lifecycle is created.
type LifecycleCreated struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
}

// LifecycleUpdated is recorded when a Lifecycle's slug, display name, or
// description changes.
type LifecycleUpdated struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
}

// LifecyclePhaseAdded is recorded when a phase is added to a Lifecycle.
type LifecyclePhaseAdded struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
	PhaseID   string            `json:"phase_id"`
}

// LifecyclePhaseUpdated is recorded when a phase's name, flags, or
// Environments change.
type LifecyclePhaseUpdated struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
	PhaseID   string            `json:"phase_id"`
}

// LifecyclePhaseRemoved is recorded when a phase is removed. The Lifecycle
// no longer holds it.
type LifecyclePhaseRemoved struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
	PhaseID   string            `json:"phase_id"`
}

// LifecyclePhaseMoved is recorded when a phase moves to another position.
type LifecyclePhaseMoved struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
	PhaseID   string            `json:"phase_id"`
}

// LifecycleArchived is recorded when a Lifecycle is archived.
type LifecycleArchived struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
}

// LifecycleRestored is recorded when an archived Lifecycle is restored.
type LifecycleRestored struct {
	Lifecycle LifecycleSnapshot `json:"lifecycle"`
}

func (LifecycleCreated) EventName() string      { return "delivery.v1.lifecycle.created" }
func (LifecycleUpdated) EventName() string      { return "delivery.v1.lifecycle.updated" }
func (LifecyclePhaseAdded) EventName() string   { return "delivery.v1.lifecycle.phase_added" }
func (LifecyclePhaseUpdated) EventName() string { return "delivery.v1.lifecycle.phase_updated" }
func (LifecyclePhaseRemoved) EventName() string { return "delivery.v1.lifecycle.phase_removed" }
func (LifecyclePhaseMoved) EventName() string   { return "delivery.v1.lifecycle.phase_moved" }
func (LifecycleArchived) EventName() string     { return "delivery.v1.lifecycle.archived" }
func (LifecycleRestored) EventName() string     { return "delivery.v1.lifecycle.restored" }
