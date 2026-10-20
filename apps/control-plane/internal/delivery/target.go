package delivery

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// Runtime names the provider a Target runs on.
type Runtime string

const RuntimeKubernetes Runtime = "kubernetes"

// Locator keys for Kubernetes Targets: the cluster's identity and the namespace.
const (
	LocatorCluster   = "cluster"
	LocatorNamespace = "namespace"
)

// Target is a concrete place an Environment runs, such as a namespace in a
// Kubernetes cluster. It holds no credentials: its runtime agent reaches the
// cluster with its own in-cluster identity. Access follows its Environment.
type Target struct {
	id               string
	organizationID   string
	environmentID    string
	slug             string
	displayName      string
	runtime          Runtime
	locator          map[string]string
	agentPrincipalID *string
	lastReportedAt   *time.Time
	createdAt        time.Time
	updatedAt        time.Time
	deletedAt        *time.Time
}

// TargetSnapshot is a Target's state, for repositories and audit records.
type TargetSnapshot struct {
	ID               string
	OrganizationID   string
	EnvironmentID    string
	Slug             string
	DisplayName      string
	Runtime          Runtime
	Locator          map[string]string
	AgentPrincipalID *string
	LastReportedAt   *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

// NewTargetProps describes a Target. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewTargetProps struct {
	OrganizationID string
	EnvironmentID  string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Runtime     Runtime
	// Locator is the runtime's non-secret address. For Kubernetes it has
	// LocatorCluster and LocatorNamespace.
	Locator map[string]string
	// AgentPrincipalID is the IAM workload principal of the runtime agent
	// that serves the Target, if one is assigned.
	AgentPrincipalID *string
	LastReportedAt   *time.Time
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
	DeletedAt        *time.Time
}

// UpdateTargetProps changes the fields that are set.
type UpdateTargetProps struct {
	Slug        *string
	DisplayName *string
	// Locator replaces the whole locator when not nil.
	Locator map[string]string
}

// NewTarget returns a Target, with a new ID unless targetID gives a stored one.
func NewTarget(props NewTargetProps, targetID *string) (*Target, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Environment{}.Prefix(), props.EnvironmentID) {
		return nil, NewErrInvalidInput("a target needs an environment ID")
	}
	if err := validateOwnID(Target{}.Prefix(), targetID); err != nil {
		return nil, err
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s, err := slugOrDerived(props.Slug, props.DisplayName)
	if err != nil {
		return nil, err
	}
	if err := validateLocator(props.Runtime, props.Locator); err != nil {
		return nil, err
	}
	agent := optionalString(props.AgentPrincipalID)

	now := clock.Now()
	t := &Target{
		organizationID:   props.OrganizationID,
		environmentID:    props.EnvironmentID,
		slug:             s,
		displayName:      props.DisplayName,
		runtime:          props.Runtime,
		locator:          copyLabels(props.Locator),
		agentPrincipalID: agent,
		lastReportedAt:   clock.CopyTime(props.LastReportedAt),
		createdAt:        timeOr(props.CreatedAt, now),
		updatedAt:        timeOr(props.UpdatedAt, now),
		deletedAt:        clock.CopyTime(props.DeletedAt),
	}
	if targetID != nil {
		t.id = *targetID
	}
	id.Assign(&t.id, t)
	return t, nil
}

// validateLocator checks a locator against its runtime. New runtimes add
// their own keys here.
func validateLocator(runtime Runtime, locator map[string]string) error {
	switch runtime {
	case RuntimeKubernetes:
		allowed := []string{LocatorCluster, LocatorNamespace}
		for k := range locator {
			if !slices.Contains(allowed, k) {
				return NewErrInvalidInput(fmt.Sprintf("a kubernetes locator has only %s; %q is not allowed", strings.Join(allowed, " and "), k))
			}
		}
		cluster := locator[LocatorCluster]
		if cluster == "" || len(cluster) > maxDisplayNameLength || strings.ContainsAny(cluster, " \t\n") {
			return NewErrInvalidInput(fmt.Sprintf("a kubernetes locator needs a %s of 1-%d characters without spaces", LocatorCluster, maxDisplayNameLength))
		}
		// Namespaces are DNS labels, which is the slug format.
		if err := slug.Validate(locator[LocatorNamespace]); err != nil {
			return NewErrInvalidInput(fmt.Sprintf("a kubernetes locator needs a %s that is a DNS label", LocatorNamespace))
		}
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("runtime %q is not supported; use kubernetes", runtime))
}

func (Target) Prefix() string                { return "tgt" }
func (t *Target) ID() string                 { return t.id }
func (t *Target) OrganizationID() string     { return t.organizationID }
func (t *Target) EnvironmentID() string      { return t.environmentID }
func (t *Target) Slug() string               { return t.slug }
func (t *Target) DisplayName() string        { return t.displayName }
func (t *Target) Runtime() Runtime           { return t.runtime }
func (t *Target) Locator() map[string]string { return copyLabels(t.locator) }
func (t *Target) AgentPrincipalID() *string  { return copyString(t.agentPrincipalID) }
func (t *Target) LastReportedAt() *time.Time { return clock.CopyTime(t.lastReportedAt) }
func (t *Target) CreatedAt() time.Time       { return t.createdAt }
func (t *Target) UpdatedAt() time.Time       { return t.updatedAt }
func (t *Target) DeletedAt() *time.Time      { return clock.CopyTime(t.deletedAt) }

func (t *Target) Archived() bool { return t.deletedAt != nil }

func (t *Target) Snapshot() TargetSnapshot {
	return TargetSnapshot{
		ID:               t.id,
		OrganizationID:   t.organizationID,
		EnvironmentID:    t.environmentID,
		Slug:             t.slug,
		DisplayName:      t.displayName,
		Runtime:          t.runtime,
		Locator:          copyLabels(t.locator),
		AgentPrincipalID: copyString(t.agentPrincipalID),
		LastReportedAt:   clock.CopyTime(t.lastReportedAt),
		CreatedAt:        t.createdAt,
		UpdatedAt:        t.updatedAt,
		DeletedAt:        clock.CopyTime(t.deletedAt),
	}
}

func (t *Target) checkActive() error {
	if t.Archived() {
		return fmt.Errorf("%w: target %s", ErrArchived, t.id)
	}
	return nil
}

// Update changes an active Target. Nothing changes if any field is invalid.
func (t *Target) Update(props UpdateTargetProps) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if props.Slug != nil {
		if err := validateSlug(*props.Slug); err != nil {
			return err
		}
	}
	if props.DisplayName != nil {
		if err := validateDisplayName(*props.DisplayName); err != nil {
			return err
		}
	}
	if props.Locator != nil {
		if err := validateLocator(t.runtime, props.Locator); err != nil {
			return err
		}
	}

	if props.Slug != nil {
		t.slug = *props.Slug
	}
	if props.DisplayName != nil {
		t.displayName = *props.DisplayName
	}
	if props.Locator != nil {
		t.locator = maps.Clone(props.Locator)
	}
	t.updatedAt = clock.Now()
	return nil
}

// AssignAgent makes principalID the runtime agent that serves the Target,
// replacing any earlier one. Only that agent may report on the Target.
func (t *Target) AssignAgent(principalID string) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if principalID == "" {
		return NewErrInvalidInput("an agent principal ID is required")
	}
	t.agentPrincipalID = &principalID
	t.lastReportedAt = nil
	t.updatedAt = clock.Now()
	return nil
}

// UnassignAgent removes an active Target's agent.
func (t *Target) UnassignAgent() error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if t.agentPrincipalID != nil {
		t.agentPrincipalID = nil
		t.lastReportedAt = nil
		t.updatedAt = clock.Now()
	}
	return nil
}

// RecordReport notes that the Target's agent is still in contact. It is
// refused for any principal but the assigned agent. It does not change
// UpdatedAt, which tracks changes to the Target's configuration.
func (t *Target) RecordReport(principalID string) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if t.agentPrincipalID == nil || *t.agentPrincipalID != principalID {
		return fmt.Errorf("%w: %s does not serve target %s", ErrNotTargetAgent, principalID, t.id)
	}
	now := clock.Now()
	t.lastReportedAt = &now
	return nil
}

// Archive soft-deletes the Target. Archiving an archived one does nothing.
func (t *Target) Archive() {
	if !t.Archived() {
		now := clock.Now()
		t.deletedAt = &now
		t.updatedAt = now
	}
}

// Restore undoes Archive. Restoring an active Target does nothing.
func (t *Target) Restore() {
	if t.Archived() {
		t.deletedAt = nil
		t.updatedAt = clock.Now()
	}
}
