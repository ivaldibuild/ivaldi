package delivery

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// Runtime names the provider a Target runs on.
type Runtime string

const RuntimeKubernetes Runtime = "kubernetes"

// Locator keys for Kubernetes Targets: the Cluster's slug and the namespace.
const (
	LocatorCluster   = "cluster"
	LocatorNamespace = "namespace"
)

// Target is a concrete place an Environment runs, such as a namespace in a
// Kubernetes cluster. It holds no credentials: the Cluster's runtime agent
// reaches the cluster with its own in-cluster identity. Access follows its
// Environment.
type Target struct {
	aggregate.Root
	organizationID string
	environmentID  string
	clusterID      string
	slug           string
	displayName    string
	runtime        Runtime
	locator        map[string]string
	// canObserve and canExecute are what the Cluster's agent last reported
	// it can do in this namespace. They are set by the agent's heartbeat,
	// never by the Target's configuration methods.
	canObserve bool
	canExecute bool
	createdAt  time.Time
	updatedAt  time.Time
	deletedAt  *time.Time
}

// TargetSnapshot is a Target's state, for repositories, audit records, and
// events. Its JSON form is part of the Target events' contract.
type TargetSnapshot struct {
	ID             string            `json:"id"`
	OrganizationID string            `json:"organization_id"`
	EnvironmentID  string            `json:"environment_id"`
	ClusterID      string            `json:"cluster_id"`
	Slug           string            `json:"slug"`
	DisplayName    string            `json:"display_name"`
	Runtime        Runtime           `json:"runtime"`
	Locator        map[string]string `json:"locator"`
	CanObserve     bool              `json:"can_observe"`
	CanExecute     bool              `json:"can_execute"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	DeletedAt      *time.Time        `json:"deleted_at"`
}

// NewTargetProps describes a Target. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewTargetProps struct {
	OrganizationID string
	EnvironmentID  string
	// ClusterID is the Cluster whose agent serves the Target. It cannot
	// change; a Target that moves is archived and created again.
	ClusterID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Runtime     Runtime
	// Locator is the runtime's non-secret address. For Kubernetes it has
	// LocatorCluster and LocatorNamespace.
	Locator    map[string]string
	CanObserve bool
	CanExecute bool
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
	DeletedAt  *time.Time
}

// UpdateTargetProps changes the fields that are set.
type UpdateTargetProps struct {
	Slug        *string
	DisplayName *string
	// Locator replaces the whole locator when not nil. Its cluster must be
	// the one the Target already names; only the namespace may change.
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
	if !id.Valid(Cluster{}.Prefix(), props.ClusterID) {
		return nil, NewErrInvalidInput("a target needs a cluster ID")
	}
	root, err := newRoot(Target{}.Prefix(), targetID)
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
	// validateLocator checks the runtime first.
	if err := validateLocator(props.Runtime, props.Locator); err != nil {
		return nil, err
	}

	now := clock.Now()
	t := &Target{
		Root:           root,
		organizationID: props.OrganizationID,
		environmentID:  props.EnvironmentID,
		clusterID:      props.ClusterID,
		slug:           s,
		displayName:    props.DisplayName,
		runtime:        props.Runtime,
		locator:        copyLabels(props.Locator),
		canObserve:     props.CanObserve,
		canExecute:     props.CanExecute,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		deletedAt:      clock.CopyTime(props.DeletedAt),
	}
	if t.IsNew() {
		t.RecordEvent(TargetCreated{t.Snapshot()})
	}
	return t, nil
}

// validateLocator checks a locator against its runtime. New runtimes add
// their own keys here.
func validateLocator(runtime Runtime, locator map[string]string) error {
	if err := validateRuntime(runtime); err != nil {
		return err
	}
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

func (Target) Prefix() string                { return "tgt" }
func (t *Target) OrganizationID() string     { return t.organizationID }
func (t *Target) EnvironmentID() string      { return t.environmentID }
func (t *Target) ClusterID() string          { return t.clusterID }
func (t *Target) Slug() string               { return t.slug }
func (t *Target) DisplayName() string        { return t.displayName }
func (t *Target) Runtime() Runtime           { return t.runtime }
func (t *Target) Locator() map[string]string { return copyLabels(t.locator) }
func (t *Target) CanObserve() bool           { return t.canObserve }
func (t *Target) CanExecute() bool           { return t.canExecute }
func (t *Target) CreatedAt() time.Time       { return t.createdAt }
func (t *Target) UpdatedAt() time.Time       { return t.updatedAt }
func (t *Target) DeletedAt() *time.Time      { return clock.CopyTime(t.deletedAt) }

func (t *Target) Archived() bool { return t.deletedAt != nil }

func (t *Target) Snapshot() TargetSnapshot {
	return TargetSnapshot{
		ID:             t.ID(),
		OrganizationID: t.organizationID,
		EnvironmentID:  t.environmentID,
		ClusterID:      t.clusterID,
		Slug:           t.slug,
		DisplayName:    t.displayName,
		Runtime:        t.runtime,
		Locator:        copyLabels(t.locator),
		CanObserve:     t.canObserve,
		CanExecute:     t.canExecute,
		CreatedAt:      t.createdAt,
		UpdatedAt:      t.updatedAt,
		DeletedAt:      clock.CopyTime(t.deletedAt),
	}
}

func (t *Target) checkActive() error {
	if t.Archived() {
		return fmt.Errorf("%w: target %s", ErrArchived, t.ID())
	}
	return nil
}

// Update changes an active Target. Nothing changes if any field is invalid.
// A Target cannot move to another Cluster: archiving it and creating a new
// one keeps its history with the Cluster it ran in.
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
		if props.Locator[LocatorCluster] != t.locator[LocatorCluster] {
			return NewErrInvalidInput("a target cannot move to another cluster; archive it and create it again")
		}
	}

	changed := anyTrue(
		set(&t.slug, props.Slug),
		set(&t.displayName, props.DisplayName),
	)
	if props.Locator != nil && !maps.Equal(props.Locator, t.locator) {
		t.locator = maps.Clone(props.Locator)
		changed = true
	}
	t.updatedAt = clock.Now()
	if changed {
		t.RecordEvent(TargetUpdated{t.Snapshot()})
	}
	return nil
}

// SetCapabilities records what the Cluster's agent last reported it can do in
// the Target's namespace, and reports whether anything changed. Only the
// service that checked the caller is the Cluster's agent may call it.
func (t *Target) SetCapabilities(canObserve, canExecute bool) bool {
	if t.canObserve == canObserve && t.canExecute == canExecute {
		return false
	}
	t.canObserve = canObserve
	t.canExecute = canExecute
	return true
}

// Archive soft-deletes the Target. Archiving an archived one does nothing.
func (t *Target) Archive() {
	if !t.Archived() {
		now := clock.Now()
		t.deletedAt = &now
		t.updatedAt = now
		t.RecordEvent(TargetArchived{t.Snapshot()})
	}
}

// Restore undoes Archive. Restoring an active Target does nothing.
func (t *Target) Restore() {
	if t.Archived() {
		t.deletedAt = nil
		t.updatedAt = clock.Now()
		t.RecordEvent(TargetRestored{t.Snapshot()})
	}
}
