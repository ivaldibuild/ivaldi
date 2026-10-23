package delivery

// Events an Artifact records. Each carries the Artifact as it is after the
// change; the audit record keeps the state before it.

// ArtifactCreated is recorded when an Artifact is declared or first observed.
type ArtifactCreated struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactUpdated is recorded when an Artifact's name, published name, or
// required flag changes.
type ArtifactUpdated struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactRegistrySet is recorded when an Artifact is pointed at a Registry,
// or at a different one.
type ArtifactRegistrySet struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactRegistryCleared is recorded when an Artifact's Registry is removed.
type ArtifactRegistryCleared struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactConfirmed is recorded when someone confirms an observed Artifact,
// making it declared.
type ArtifactConfirmed struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactArchived is recorded when an Artifact is archived.
type ArtifactArchived struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

// ArtifactRestored is recorded when an archived Artifact is restored.
type ArtifactRestored struct {
	Artifact ArtifactSnapshot `json:"artifact"`
}

func (ArtifactCreated) EventName() string         { return "delivery.v1.artifact.created" }
func (ArtifactUpdated) EventName() string         { return "delivery.v1.artifact.updated" }
func (ArtifactRegistrySet) EventName() string     { return "delivery.v1.artifact.registry_set" }
func (ArtifactRegistryCleared) EventName() string { return "delivery.v1.artifact.registry_cleared" }
func (ArtifactConfirmed) EventName() string       { return "delivery.v1.artifact.confirmed" }
func (ArtifactArchived) EventName() string        { return "delivery.v1.artifact.archived" }
func (ArtifactRestored) EventName() string        { return "delivery.v1.artifact.restored" }
