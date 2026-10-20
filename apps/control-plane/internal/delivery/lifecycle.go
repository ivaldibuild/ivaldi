package delivery

import (
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Lifecycle is an ordered path through a Workspace's Environments. It owns
// its phases: they change only through the Lifecycle's methods, which keep
// every Environment in at most one phase.
type Lifecycle struct {
	id             string
	organizationID string
	workspaceID    string
	slug           string
	displayName    string
	description    *string
	phases         []*LifecyclePhase
	createdAt      time.Time
	updatedAt      time.Time
	deletedAt      *time.Time
}

// LifecyclePhase is one step in a Lifecycle. Its position is its index in
// the Lifecycle's phases.
type LifecyclePhase struct {
	id              string
	name            string
	optional        bool
	minEnvironments *int
	environmentIDs  []string
	createdAt       time.Time
	updatedAt       time.Time
}

// LifecycleSnapshot is a Lifecycle's state, for repositories and audit records.
type LifecycleSnapshot struct {
	ID             string
	OrganizationID string
	WorkspaceID    string
	Slug           string
	DisplayName    string
	Description    *string
	// Phases are in order; each one's Position is its index.
	Phases    []LifecyclePhaseSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// LifecyclePhaseSnapshot is a phase's state.
type LifecyclePhaseSnapshot struct {
	ID       string
	Position int
	Name     string
	Optional bool
	// MinEnvironments is how many of the phase's Environments need a
	// successful Deployment before the next phase opens; nil means all.
	MinEnvironments *int
	// EnvironmentIDs are sorted: order within a phase means nothing.
	EnvironmentIDs []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewLifecycleProps describes a Lifecycle. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewLifecycleProps struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description *string
	// Phases are in order.
	Phases    []NewLifecyclePhaseProps
	CreatedAt *time.Time
	UpdatedAt *time.Time
	DeletedAt *time.Time
}

// NewLifecyclePhaseProps describes a phase given to NewLifecycle. A new one
// leaves ID and the timestamps nil; a stored one sets them.
type NewLifecyclePhaseProps struct {
	ID              *string
	Name            string
	Optional        bool
	MinEnvironments *int
	EnvironmentIDs  []string
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

// LifecyclePhaseProps is the content of a phase added to or changed in a Lifecycle.
type LifecyclePhaseProps struct {
	Name            string
	Optional        bool
	MinEnvironments *int
	EnvironmentIDs  []string
}

// UpdateLifecycleProps changes the fields that are set. An empty
// Description removes it.
type UpdateLifecycleProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// NewLifecycle returns a Lifecycle, with new IDs for it and its phases
// unless they are given.
func NewLifecycle(props NewLifecycleProps, lifecycleID *string) (*Lifecycle, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateRequired("workspace ID", props.WorkspaceID); err != nil {
		return nil, err
	}
	if err := validateOwnID(Lifecycle{}.Prefix(), lifecycleID); err != nil {
		return nil, err
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s, err := slugOrDerived(props.Slug, props.DisplayName)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	l := &Lifecycle{
		organizationID: props.OrganizationID,
		workspaceID:    props.WorkspaceID,
		slug:           s,
		displayName:    props.DisplayName,
		description:    optionalString(props.Description),
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		deletedAt:      clock.CopyTime(props.DeletedAt),
	}
	for _, p := range props.Phases {
		phase, err := newLifecyclePhase(p, now)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(l.phases, func(o *LifecyclePhase) bool { return o.id == phase.id }) {
			return nil, NewErrInvalidInput(fmt.Sprintf("phase %s appears twice", phase.id))
		}
		l.phases = append(l.phases, phase)
	}
	if err := checkPhases(l.phases); err != nil {
		return nil, err
	}
	if lifecycleID != nil {
		l.id = *lifecycleID
	}
	id.Assign(&l.id, l)

	return l, nil
}

func newLifecyclePhase(p NewLifecyclePhaseProps, now time.Time) (*LifecyclePhase, error) {
	if err := validateOwnID(LifecyclePhase{}.Prefix(), p.ID); err != nil {
		return nil, err
	}
	props := LifecyclePhaseProps{Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs}
	if err := props.validate(); err != nil {
		return nil, err
	}
	phase := &LifecyclePhase{
		name:            p.Name,
		optional:        p.Optional,
		minEnvironments: copyInt(p.MinEnvironments),
		environmentIDs:  sortedStrings(p.EnvironmentIDs),
		createdAt:       timeOr(p.CreatedAt, now),
		updatedAt:       timeOr(p.UpdatedAt, now),
	}
	if p.ID != nil {
		phase.id = *p.ID
	}
	id.Assign(&phase.id, phase)
	return phase, nil
}

func (p LifecyclePhaseProps) validate() error {
	if err := validateDisplayName(p.Name); err != nil {
		return NewErrInvalidInput(fmt.Sprintf("phase name must be 1-%d characters", maxDisplayNameLength))
	}
	if len(p.EnvironmentIDs) == 0 {
		return NewErrInvalidInput(fmt.Sprintf("phase %q needs at least one environment", p.Name))
	}
	for i, envID := range p.EnvironmentIDs {
		if !id.Valid(Environment{}.Prefix(), envID) {
			return NewErrInvalidInput(fmt.Sprintf("%q is not an environment ID", envID))
		}
		if slices.Contains(p.EnvironmentIDs[:i], envID) {
			return NewErrInvalidInput(fmt.Sprintf("environment %s appears twice in phase %q", envID, p.Name))
		}
	}
	if p.MinEnvironments != nil && (*p.MinEnvironments < 1 || *p.MinEnvironments > len(p.EnvironmentIDs)) {
		return NewErrInvalidInput(fmt.Sprintf("phase %q must require between 1 and %d environments", p.Name, len(p.EnvironmentIDs)))
	}
	return nil
}

// checkPhases checks the rules across phases: a bounded number of them, and
// each Environment in at most one.
func checkPhases(phases []*LifecyclePhase) error {
	if len(phases) > maxPhases {
		return NewErrInvalidInput(fmt.Sprintf("a lifecycle can have at most %d phases", maxPhases))
	}
	seen := map[string]string{}
	for _, p := range phases {
		for _, envID := range p.environmentIDs {
			if other, ok := seen[envID]; ok {
				return NewErrInvalidInput(fmt.Sprintf("environment %s is in phases %q and %q; it can be in only one", envID, other, p.name))
			}
			seen[envID] = p.name
		}
	}
	return nil
}

func (Lifecycle) Prefix() string      { return "lfc" }
func (LifecyclePhase) Prefix() string { return "lfp" }

func (l *Lifecycle) ID() string             { return l.id }
func (l *Lifecycle) OrganizationID() string { return l.organizationID }
func (l *Lifecycle) WorkspaceID() string    { return l.workspaceID }
func (l *Lifecycle) Slug() string           { return l.slug }
func (l *Lifecycle) DisplayName() string    { return l.displayName }
func (l *Lifecycle) Description() *string   { return copyString(l.description) }
func (l *Lifecycle) CreatedAt() time.Time   { return l.createdAt }
func (l *Lifecycle) UpdatedAt() time.Time   { return l.updatedAt }
func (l *Lifecycle) DeletedAt() *time.Time  { return clock.CopyTime(l.deletedAt) }

func (l *Lifecycle) Archived() bool { return l.deletedAt != nil }

// Phases returns the phases in order.
func (l *Lifecycle) Phases() []LifecyclePhaseSnapshot {
	out := make([]LifecyclePhaseSnapshot, len(l.phases))
	for i, p := range l.phases {
		out[i] = p.snapshot(i)
	}
	return out
}

// EnvironmentIDs returns every Environment in the Lifecycle, in phase order.
func (l *Lifecycle) EnvironmentIDs() []string {
	var out []string
	for _, p := range l.phases {
		out = append(out, p.environmentIDs...)
	}
	return out
}

func (p *LifecyclePhase) snapshot(position int) LifecyclePhaseSnapshot {
	return LifecyclePhaseSnapshot{
		ID:              p.id,
		Position:        position,
		Name:            p.name,
		Optional:        p.optional,
		MinEnvironments: copyInt(p.minEnvironments),
		EnvironmentIDs:  copyStrings(p.environmentIDs),
		CreatedAt:       p.createdAt,
		UpdatedAt:       p.updatedAt,
	}
}

func (l *Lifecycle) Snapshot() LifecycleSnapshot {
	return LifecycleSnapshot{
		ID:             l.id,
		OrganizationID: l.organizationID,
		WorkspaceID:    l.workspaceID,
		Slug:           l.slug,
		DisplayName:    l.displayName,
		Description:    copyString(l.description),
		Phases:         l.Phases(),
		CreatedAt:      l.createdAt,
		UpdatedAt:      l.updatedAt,
		DeletedAt:      clock.CopyTime(l.deletedAt),
	}
}

func (l *Lifecycle) checkActive() error {
	if l.Archived() {
		return fmt.Errorf("%w: lifecycle %s", ErrArchived, l.id)
	}
	return nil
}

// Update changes an active Lifecycle. Nothing changes if any field is invalid.
func (l *Lifecycle) Update(props UpdateLifecycleProps) error {
	if err := l.checkActive(); err != nil {
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

	if props.Slug != nil {
		l.slug = *props.Slug
	}
	if props.DisplayName != nil {
		l.displayName = *props.DisplayName
	}
	if props.Description != nil {
		l.description = optionalString(props.Description)
	}
	l.updatedAt = clock.Now()
	return nil
}

// AddPhase appends a phase and returns it.
func (l *Lifecycle) AddPhase(props LifecyclePhaseProps) (LifecyclePhaseSnapshot, error) {
	if err := l.checkActive(); err != nil {
		return LifecyclePhaseSnapshot{}, err
	}
	now := clock.Now()
	phase, err := newLifecyclePhase(NewLifecyclePhaseProps{
		Name: props.Name, Optional: props.Optional, MinEnvironments: props.MinEnvironments, EnvironmentIDs: props.EnvironmentIDs,
	}, now)
	if err != nil {
		return LifecyclePhaseSnapshot{}, err
	}
	phases := append(slices.Clone(l.phases), phase)
	if err := checkPhases(phases); err != nil {
		return LifecyclePhaseSnapshot{}, err
	}
	l.phases = phases
	l.updatedAt = now
	return phase.snapshot(len(phases) - 1), nil
}

// UpdatePhase replaces a phase's name, flags, and Environments.
func (l *Lifecycle) UpdatePhase(phaseID string, props LifecyclePhaseProps) error {
	if err := l.checkActive(); err != nil {
		return err
	}
	i, err := l.phaseIndex(phaseID)
	if err != nil {
		return err
	}
	if err := props.validate(); err != nil {
		return err
	}
	now := clock.Now()
	updated := *l.phases[i]
	updated.name = props.Name
	updated.optional = props.Optional
	updated.minEnvironments = copyInt(props.MinEnvironments)
	updated.environmentIDs = sortedStrings(props.EnvironmentIDs)
	updated.updatedAt = now

	phases := slices.Clone(l.phases)
	phases[i] = &updated
	if err := checkPhases(phases); err != nil {
		return err
	}
	l.phases = phases
	l.updatedAt = now
	return nil
}

// RemovePhase removes a phase; the phases after it move up.
func (l *Lifecycle) RemovePhase(phaseID string) error {
	if err := l.checkActive(); err != nil {
		return err
	}
	i, err := l.phaseIndex(phaseID)
	if err != nil {
		return err
	}
	l.phases = slices.Delete(slices.Clone(l.phases), i, i+1)
	l.updatedAt = clock.Now()
	return nil
}

// MovePhase moves a phase to position, shifting the phases in between.
func (l *Lifecycle) MovePhase(phaseID string, position int) error {
	if err := l.checkActive(); err != nil {
		return err
	}
	i, err := l.phaseIndex(phaseID)
	if err != nil {
		return err
	}
	if position < 0 || position >= len(l.phases) {
		return NewErrInvalidInput(fmt.Sprintf("position must be between 0 and %d", len(l.phases)-1))
	}
	phase := l.phases[i]
	phases := slices.Delete(slices.Clone(l.phases), i, i+1)
	l.phases = slices.Insert(phases, position, phase)
	l.updatedAt = clock.Now()
	return nil
}

func (l *Lifecycle) phaseIndex(phaseID string) (int, error) {
	i := slices.IndexFunc(l.phases, func(p *LifecyclePhase) bool { return p.id == phaseID })
	if i < 0 {
		return 0, fmt.Errorf("%w: phase %s in lifecycle %s", ErrNotFound, phaseID, l.id)
	}
	return i, nil
}

// Archive soft-deletes the Lifecycle. Archiving an archived one does nothing.
func (l *Lifecycle) Archive() {
	if !l.Archived() {
		now := clock.Now()
		l.deletedAt = &now
		l.updatedAt = now
	}
}

// Restore undoes Archive. Restoring an active Lifecycle does nothing.
func (l *Lifecycle) Restore() {
	if l.Archived() {
		l.deletedAt = nil
		l.updatedAt = clock.Now()
	}
}
