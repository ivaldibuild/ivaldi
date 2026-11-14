package delivery

// Events a Cluster records. Each carries the Cluster as it is after the
// change; the audit record keeps the state before it. Agent heartbeats and
// refused enrolments record no event: they are contact, not changes to the
// Cluster.

// ClusterCreated is recorded when a Cluster is created.
type ClusterCreated struct {
	Cluster ClusterSnapshot `json:"cluster"`
}

// ClusterUpdated is recorded when a Cluster's display name changes.
type ClusterUpdated struct {
	Cluster ClusterSnapshot `json:"cluster"`
}

// ClusterAgentAssigned is recorded when a runtime agent is enrolled for a
// Cluster, replacing PreviousAgentPrincipalID if there was one.
type ClusterAgentAssigned struct {
	Cluster                  ClusterSnapshot `json:"cluster"`
	PreviousAgentPrincipalID *string         `json:"previous_agent_principal_id"`
}

// ClusterAgentUnassigned is recorded when a Cluster's runtime agent is removed.
type ClusterAgentUnassigned struct {
	Cluster                  ClusterSnapshot `json:"cluster"`
	PreviousAgentPrincipalID string          `json:"previous_agent_principal_id"`
}

// ClusterArchived is recorded when a Cluster is archived.
type ClusterArchived struct {
	Cluster ClusterSnapshot `json:"cluster"`
}

// ClusterRestored is recorded when an archived Cluster is restored.
type ClusterRestored struct {
	Cluster ClusterSnapshot `json:"cluster"`
}

func (ClusterCreated) EventName() string         { return "delivery.v1.cluster.created" }
func (ClusterUpdated) EventName() string         { return "delivery.v1.cluster.updated" }
func (ClusterAgentAssigned) EventName() string   { return "delivery.v1.cluster.agent_assigned" }
func (ClusterAgentUnassigned) EventName() string { return "delivery.v1.cluster.agent_unassigned" }
func (ClusterArchived) EventName() string        { return "delivery.v1.cluster.archived" }
func (ClusterRestored) EventName() string        { return "delivery.v1.cluster.restored" }
