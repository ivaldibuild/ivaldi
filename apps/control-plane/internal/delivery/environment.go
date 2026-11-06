package delivery

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// Tier classifies an Environment so policy rules can match on it.
type Tier string

const (
	TierDevelopment Tier = "development"
	TierStaging     Tier = "staging"
	TierProduction  Tier = "production"
	TierOther       Tier = "other"
)

func (t Tier) Valid() (bool, error) {
	switch t {
	case TierDevelopment, TierStaging, TierProduction, TierOther:
		return true, nil
	default:
		return false, fmt.Errorf("invalid tier %s", t)
	}
}

// Environment is a named stage in a Workspace, such as staging or
// production. It is registered with IAM as a resource under its Workspace.
type Environment struct {
	aggregate.Root
	organizationID   string
	workspaceID      string
	slug             string
	displayName      string
	tier             Tier
	executionEnabled bool
	createdAt        time.Time
	updatedAt        time.Time
	deletedAt        *time.Time
}

// EnvironmentSnapshot is an Environment's state, for repositories, audit records, and
// events. Its JSON form is part of the Environment events' contract.
type EnvironmentSnapshot struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organization_id"`
	WorkspaceID      string     `json:"workspace_id"`
	Slug             string     `json:"slug"`
	DisplayName      string     `json:"display_name"`
	Tier             Tier       `json:"tier" enum:"development,staging,production,other"`
	ExecutionEnabled bool       `json:"execution_enabled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
}

// NewEnvironmentProps describes an Environment. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewEnvironmentProps struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug             string
	DisplayName      string
	Tier             Tier
	ExecutionEnabled bool
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
	DeletedAt        *time.Time
}

// UpdateEnvironmentProps changes the fields that are set.
type UpdateEnvironmentProps struct {
	Slug             *string
	DisplayName      *string
	Tier             *Tier
	ExecutionEnabled *bool
}

// NewEnvironment returns an Environment, with a new ID unless environmentID
// gives a stored one.
func NewEnvironment(props NewEnvironmentProps, environmentID *string) (*Environment, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateRequired("workspace ID", props.WorkspaceID); err != nil {
		return nil, err
	}
	root, err := newRoot(Environment{}.Prefix(), environmentID)
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
	if err := validateTier(props.Tier); err != nil {
		return nil, err
	}

	now := clock.Now()
	e := &Environment{
		Root:             root,
		organizationID:   props.OrganizationID,
		workspaceID:      props.WorkspaceID,
		slug:             s,
		displayName:      props.DisplayName,
		tier:             props.Tier,
		executionEnabled: props.ExecutionEnabled,
		createdAt:        timeOr(props.CreatedAt, now),
		updatedAt:        timeOr(props.UpdatedAt, now),
		deletedAt:        clock.CopyTime(props.DeletedAt),
	}
	if e.IsNew() {
		e.RecordEvent(EnvironmentCreated{e.Snapshot()})
	}
	return e, nil
}

func validateTier(t Tier) error {
	switch t {
	case TierDevelopment, TierStaging, TierProduction, TierOther:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("tier %q must be development, staging, production, or other", t))
}

func (Environment) Prefix() string            { return "env" }
func (e *Environment) OrganizationID() string { return e.organizationID }
func (e *Environment) WorkspaceID() string    { return e.workspaceID }
func (e *Environment) Slug() string           { return e.slug }
func (e *Environment) DisplayName() string    { return e.displayName }
func (e *Environment) Tier() Tier             { return e.tier }
func (e *Environment) ExecutionEnabled() bool { return e.executionEnabled }
func (e *Environment) CreatedAt() time.Time   { return e.createdAt }
func (e *Environment) UpdatedAt() time.Time   { return e.updatedAt }
func (e *Environment) DeletedAt() *time.Time  { return clock.CopyTime(e.deletedAt) }

func (e *Environment) Archived() bool { return e.deletedAt != nil }

func (e *Environment) Snapshot() EnvironmentSnapshot {
	return EnvironmentSnapshot{
		ID:               e.ID(),
		OrganizationID:   e.organizationID,
		WorkspaceID:      e.workspaceID,
		Slug:             e.slug,
		DisplayName:      e.displayName,
		Tier:             e.tier,
		ExecutionEnabled: e.executionEnabled,
		CreatedAt:        e.createdAt,
		UpdatedAt:        e.updatedAt,
		DeletedAt:        clock.CopyTime(e.deletedAt),
	}
}

// Update changes an active Environment. Nothing changes if any field is invalid.
func (e *Environment) Update(props UpdateEnvironmentProps) error {
	if e.Archived() {
		return fmt.Errorf("%w: environment %s", ErrArchived, e.ID())
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
	if props.Tier != nil {
		if err := validateTier(*props.Tier); err != nil {
			return err
		}
	}

	changed := anyTrue(
		set(&e.slug, props.Slug),
		set(&e.displayName, props.DisplayName),
		set(&e.tier, props.Tier),
		set(&e.executionEnabled, props.ExecutionEnabled),
	)
	e.updatedAt = clock.Now()
	if changed {
		e.RecordEvent(EnvironmentUpdated{e.Snapshot()})
	}
	return nil
}

// Archive soft-deletes the Environment. Archiving an archived one does nothing.
func (e *Environment) Archive() {
	if !e.Archived() {
		now := clock.Now()
		e.deletedAt = &now
		e.updatedAt = now
		e.RecordEvent(EnvironmentArchived{e.Snapshot()})
	}
}

// Restore undoes Archive. Restoring an active Environment does nothing.
func (e *Environment) Restore() {
	if e.Archived() {
		e.deletedAt = nil
		e.updatedAt = clock.Now()
		e.RecordEvent(EnvironmentRestored{e.Snapshot()})
	}
}
