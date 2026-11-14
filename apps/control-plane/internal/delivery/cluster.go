package delivery

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// AgentFailure is a refused agent enrolment, kept for the operator. It names
// no credential: the reason says what to fix, and may name the Cluster whose
// agent already uses the identity. Only cluster.read holders see it; the
// enrolment token's holder gets a generic refusal.
type AgentFailure struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Cluster is a Kubernetes cluster an Organization deploys to. One runtime
// agent serves the whole Cluster, and through it every Target in it. It
// belongs to the Organization rather than a Workspace, because one cluster
// usually hosts several Workspaces' Environments.
//
// A Cluster holds no credentials: its agent reaches it with its own
// in-cluster identity (ADR 0014).
type Cluster struct {
	aggregate.Root
	organizationID    string
	slug              string
	displayName       string
	runtime           Runtime
	agentPrincipalID  *string
	agentCapabilities map[string]any
	agentLastFailure  *AgentFailure
	lastReportedAt    *time.Time
	createdAt         time.Time
	updatedAt         time.Time
	deletedAt         *time.Time
}

// ClusterSnapshot is a Cluster's state, for repositories, audit records, and
// events. Its JSON form is part of the Cluster events' contract.
type ClusterSnapshot struct {
	ID                string         `json:"id"`
	OrganizationID    string         `json:"organization_id"`
	Slug              string         `json:"slug"`
	DisplayName       string         `json:"display_name"`
	Runtime           Runtime        `json:"runtime"`
	AgentPrincipalID  *string        `json:"agent_principal_id"`
	AgentCapabilities map[string]any `json:"agent_capabilities"`
	AgentLastFailure  *AgentFailure  `json:"agent_last_failure"`
	LastReportedAt    *time.Time     `json:"last_reported_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         *time.Time     `json:"deleted_at"`
}

// NewClusterProps describes a Cluster. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewClusterProps struct {
	OrganizationID string
	// Slug is derived from DisplayName when empty. It cannot change later.
	Slug        string
	DisplayName string
	Runtime     Runtime
	// AgentPrincipalID is the IAM workload principal of the runtime agent,
	// if one is enrolled.
	AgentPrincipalID *string
	// AgentCapabilities are the cluster-wide facts the agent last reported.
	AgentCapabilities map[string]any
	// AgentLastFailure is the last refused enrolment, if any.
	AgentLastFailure *AgentFailure
	LastReportedAt   *time.Time
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
	DeletedAt        *time.Time
}

// UpdateClusterProps changes the fields that are set. A Cluster's slug and
// runtime do not change.
type UpdateClusterProps struct {
	DisplayName *string
}

// NewCluster returns a Cluster, with a new ID unless clusterID gives a stored
// one.
func NewCluster(props NewClusterProps, clusterID *string) (*Cluster, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(Cluster{}.Prefix(), clusterID)
	if err != nil {
		return nil, err
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s, err := slugOrDerived(props.Slug, props.DisplayName)
	if err != nil {
		return nil, err
	}
	if err := validateRuntime(props.Runtime); err != nil {
		return nil, err
	}
	capabilities, err := copyCapabilities(props.AgentCapabilities)
	if err != nil {
		return nil, err
	}
	failure, err := copyAgentFailure(props.AgentLastFailure)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	c := &Cluster{
		Root:              root,
		organizationID:    props.OrganizationID,
		slug:              s,
		displayName:       props.DisplayName,
		runtime:           props.Runtime,
		agentPrincipalID:  optional.OfPtr(props.AgentPrincipalID),
		agentCapabilities: capabilities,
		agentLastFailure:  failure,
		lastReportedAt:    clock.CopyTime(props.LastReportedAt),
		createdAt:         timeOr(props.CreatedAt, now),
		updatedAt:         timeOr(props.UpdatedAt, now),
		deletedAt:         clock.CopyTime(props.DeletedAt),
	}
	if c.IsNew() {
		c.RecordEvent(ClusterCreated{c.Snapshot()})
	}
	return c, nil
}

// validateRuntime checks a runtime against the ones delivery serves. New
// runtimes add themselves here.
func validateRuntime(runtime Runtime) error {
	switch runtime {
	case RuntimeKubernetes:
		return nil
	default:
		return NewErrInvalidInput(fmt.Sprintf("runtime %q is not supported; use kubernetes", runtime))
	}
}

// copyCapabilities validates that capabilities are a JSON object of bounded
// size and copies them, so the aggregate cannot be changed through the map a
// caller passed in. An empty map is the agent's "reported nothing yet". The
// copy goes through JSON so a stored report is exactly what a reload gives.
func copyCapabilities(capabilities map[string]any) (map[string]any, error) {
	if len(capabilities) == 0 {
		return map[string]any{}, nil
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return nil, NewErrInvalidInput("agent capabilities must be a JSON object")
	}
	if len(encoded) > maxClusterCapabilitiesBytes {
		return nil, NewErrInvalidInput(fmt.Sprintf("agent capabilities must be at most %d bytes", maxClusterCapabilitiesBytes))
	}
	var copied map[string]any
	if err := json.Unmarshal(encoded, &copied); err != nil {
		return nil, NewErrInvalidInput("agent capabilities must be a JSON object")
	}
	return copied, nil
}

// cloneCapabilities deep-copies capabilities that copyCapabilities already
// accepted, so a caller cannot change the Cluster through a nested list or
// object. After the JSON round trip, values are only objects, lists, and
// scalars.
func cloneCapabilities(capabilities map[string]any) map[string]any {
	copied := make(map[string]any, len(capabilities))
	for k, v := range capabilities {
		copied[k] = cloneJSONValue(v)
	}
	return copied
}

func cloneJSONValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return cloneCapabilities(v)
	case []any:
		copied := make([]any, len(v))
		for i, item := range v {
			copied[i] = cloneJSONValue(item)
		}
		return copied
	default:
		return v
	}
}

func copyAgentFailure(failure *AgentFailure) (*AgentFailure, error) {
	if failure == nil {
		return nil, nil
	}
	if failure.Reason == "" || len(failure.Reason) > maxAgentFailureReasonLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("an agent failure reason must be 1-%d characters", maxAgentFailureReasonLength))
	}
	copied := *failure
	return &copied, nil
}

func (Cluster) Prefix() string                       { return "cls" }
func (c *Cluster) OrganizationID() string            { return c.organizationID }
func (c *Cluster) Slug() string                      { return c.slug }
func (c *Cluster) DisplayName() string               { return c.displayName }
func (c *Cluster) Runtime() Runtime                  { return c.runtime }
func (c *Cluster) AgentPrincipalID() *string         { return copyString(c.agentPrincipalID) }
func (c *Cluster) AgentCapabilities() map[string]any { return cloneCapabilities(c.agentCapabilities) }
func (c *Cluster) LastReportedAt() *time.Time        { return clock.CopyTime(c.lastReportedAt) }
func (c *Cluster) CreatedAt() time.Time              { return c.createdAt }
func (c *Cluster) UpdatedAt() time.Time              { return c.updatedAt }
func (c *Cluster) DeletedAt() *time.Time             { return clock.CopyTime(c.deletedAt) }

func (c *Cluster) AgentLastFailure() *AgentFailure {
	if c.agentLastFailure == nil {
		return nil
	}
	copied := *c.agentLastFailure
	return &copied
}

func (c *Cluster) Archived() bool { return c.deletedAt != nil }

func (c *Cluster) Snapshot() ClusterSnapshot {
	return ClusterSnapshot{
		ID:                c.ID(),
		OrganizationID:    c.organizationID,
		Slug:              c.slug,
		DisplayName:       c.displayName,
		Runtime:           c.runtime,
		AgentPrincipalID:  copyString(c.agentPrincipalID),
		AgentCapabilities: cloneCapabilities(c.agentCapabilities),
		AgentLastFailure:  c.AgentLastFailure(),
		LastReportedAt:    clock.CopyTime(c.lastReportedAt),
		CreatedAt:         c.createdAt,
		UpdatedAt:         c.updatedAt,
		DeletedAt:         clock.CopyTime(c.deletedAt),
	}
}

func (c *Cluster) checkActive() error {
	if c.Archived() {
		return fmt.Errorf("%w: cluster %s", ErrArchived, c.ID())
	}
	return nil
}

// Update changes an active Cluster's display name. Its slug and runtime do
// not change, so Targets that name the slug keep resolving.
func (c *Cluster) Update(props UpdateClusterProps) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if props.DisplayName != nil {
		if err := validateDisplayName(*props.DisplayName); err != nil {
			return err
		}
	}

	changed := set(&c.displayName, props.DisplayName)
	c.updatedAt = clock.Now()
	if changed {
		c.RecordEvent(ClusterUpdated{c.Snapshot()})
	}
	return nil
}

// AssignAgent makes principalID the Cluster's runtime agent, replacing any
// earlier one. A new agent has reported nothing, so the previous agent's
// capabilities, liveness, and failure are cleared. Only this agent may report
// on the Cluster and its Targets.
func (c *Cluster) AssignAgent(principalID string) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if principalID == "" {
		return NewErrInvalidInput("an agent principal ID is required")
	}
	previous := c.agentPrincipalID
	c.agentPrincipalID = &principalID
	c.agentCapabilities = map[string]any{}
	c.agentLastFailure = nil
	c.lastReportedAt = nil
	c.updatedAt = clock.Now()
	if !equalPtr(previous, &principalID) {
		c.RecordEvent(ClusterAgentAssigned{Cluster: c.Snapshot(), PreviousAgentPrincipalID: previous})
	}
	return nil
}

// UnassignAgent removes an active Cluster's agent.
func (c *Cluster) UnassignAgent() error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if c.agentPrincipalID != nil {
		previous := *c.agentPrincipalID
		c.agentPrincipalID = nil
		c.agentCapabilities = map[string]any{}
		c.agentLastFailure = nil
		c.lastReportedAt = nil
		c.updatedAt = clock.Now()
		c.RecordEvent(ClusterAgentUnassigned{Cluster: c.Snapshot(), PreviousAgentPrincipalID: previous})
	}
	return nil
}

// RecordHeartbeat notes that the Cluster's agent is in contact and records
// the cluster-wide facts it reported. It is refused for any principal but the
// assigned agent. It does not change UpdatedAt, which tracks changes to the
// Cluster's configuration, and records no event: contact is not a change.
func (c *Cluster) RecordHeartbeat(agentPrincipalID string, capabilities map[string]any) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if c.agentPrincipalID == nil || *c.agentPrincipalID != agentPrincipalID {
		return fmt.Errorf("%w: %s does not serve cluster %s", ErrNotClusterAgent, agentPrincipalID, c.ID())
	}
	copied, err := copyCapabilities(capabilities)
	if err != nil {
		return err
	}
	c.agentCapabilities = copied
	now := clock.Now()
	c.lastReportedAt = &now
	return nil
}

// RecordAgentFailure notes a refused enrolment for the Cluster, so the person
// enrolling can see why. It records no event and does not change UpdatedAt:
// the other Cluster's slug is for the operator, not a configuration change.
func (c *Cluster) RecordAgentFailure(reason string) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if reason == "" || len(reason) > maxAgentFailureReasonLength {
		return NewErrInvalidInput(fmt.Sprintf("an agent failure reason must be 1-%d characters", maxAgentFailureReasonLength))
	}
	c.agentLastFailure = &AgentFailure{Reason: reason, At: clock.Now()}
	return nil
}

// Archive soft-deletes the Cluster and clears its agent. It returns the
// principal it cleared, if any, so the service can suspend it in IAM.
// Archiving an archived Cluster does nothing.
func (c *Cluster) Archive() *string {
	if c.Archived() {
		return nil
	}
	previous := copyString(c.agentPrincipalID)
	now := clock.Now()
	c.deletedAt = &now
	c.updatedAt = now
	c.agentPrincipalID = nil
	c.agentCapabilities = map[string]any{}
	c.agentLastFailure = nil
	c.lastReportedAt = nil
	c.RecordEvent(ClusterArchived{c.Snapshot()})
	return previous
}

// Restore undoes Archive. A restored Cluster has no agent until a new
// enrolment token is redeemed. Restoring an active Cluster does nothing.
func (c *Cluster) Restore() {
	if c.Archived() {
		c.deletedAt = nil
		c.updatedAt = clock.Now()
		c.RecordEvent(ClusterRestored{c.Snapshot()})
	}
}
