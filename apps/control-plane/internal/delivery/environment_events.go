package delivery

// Events an Environment records. Each carries the Environment as it is
// after the change; the audit record keeps the state before it.

// EnvironmentCreated is recorded when an Environment is created.
type EnvironmentCreated struct {
	Environment EnvironmentSnapshot `json:"environment"`
}

// EnvironmentUpdated is recorded when an Environment's slug, display name,
// tier, or execution setting changes.
type EnvironmentUpdated struct {
	Environment EnvironmentSnapshot `json:"environment"`
}

// EnvironmentArchived is recorded when an Environment is archived.
type EnvironmentArchived struct {
	Environment EnvironmentSnapshot `json:"environment"`
}

// EnvironmentRestored is recorded when an archived Environment is restored.
type EnvironmentRestored struct {
	Environment EnvironmentSnapshot `json:"environment"`
}

func (EnvironmentCreated) EventName() string  { return "delivery.v1.environment.created" }
func (EnvironmentUpdated) EventName() string  { return "delivery.v1.environment.updated" }
func (EnvironmentArchived) EventName() string { return "delivery.v1.environment.archived" }
func (EnvironmentRestored) EventName() string { return "delivery.v1.environment.restored" }
