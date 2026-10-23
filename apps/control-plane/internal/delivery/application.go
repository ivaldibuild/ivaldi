package delivery

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// WorkloadSelectorLabel is the label an Application's workloads carry when
// its workload selector is left to the default.
const WorkloadSelectorLabel = "ivaldi.build/application"

// Application is a deployable unit in a Project, such as an API or a
// worker. It is owned by a Team, which is responsible for it; ownership
// grants no access, which follows the Project. Applications are archived,
// never deleted, because Deployments refer to them.
type Application struct {
	aggregate.Root
	organizationID   string
	workspaceID      string
	projectID        string
	ownerTeamID      string
	slug             string
	displayName      string
	sourceID         *string
	workloadSelector map[string]string
	createdAt        time.Time
	updatedAt        time.Time
	archivedAt       *time.Time
}

// ApplicationSnapshot is an Application's state, for repositories, audit
// records, and events. Its JSON form is part of the Application events'
// contract, so a change to it that consumers would misread needs new event
// versions.
type ApplicationSnapshot struct {
	ID               string            `json:"id"`
	OrganizationID   string            `json:"organization_id"`
	WorkspaceID      string            `json:"workspace_id"`
	ProjectID        string            `json:"project_id"`
	OwnerTeamID      string            `json:"owner_team_id"`
	Slug             string            `json:"slug"`
	DisplayName      string            `json:"display_name"`
	SourceID         *string           `json:"source_id"`
	WorkloadSelector map[string]string `json:"workload_selector"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ArchivedAt       *time.Time        `json:"archived_at"`
}

// NewApplicationProps describes an Application. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewApplicationProps struct {
	OrganizationID string
	// WorkspaceID is copied from the Project.
	WorkspaceID string
	ProjectID   string
	OwnerTeamID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	// SourceID names the Source the Application is usually built from.
	SourceID *string
	// WorkloadSelector tells the runtime agent which workloads are the
	// Application's. Empty means the label WorkloadSelectorLabel=<ID>.
	WorkloadSelector map[string]string
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
	ArchivedAt       *time.Time
}

// UpdateApplicationProps changes the fields that are set. An empty, non-nil
// WorkloadSelector resets it to the default.
type UpdateApplicationProps struct {
	Slug             *string
	DisplayName      *string
	WorkloadSelector map[string]string
}

// NewApplication returns an Application, with a new ID unless
// applicationID gives a stored one.
func NewApplication(props NewApplicationProps, applicationID *string) (*Application, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}

	if err := validateRequired("workspace ID", props.WorkspaceID); err != nil {
		return nil, err
	}

	if err := validateRequired("project ID", props.ProjectID); err != nil {
		return nil, err
	}

	if err := validateRequired("owning team ID", props.OwnerTeamID); err != nil {
		return nil, err
	}

	root, err := newRoot(Application{}.Prefix(), applicationID)
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

	if len(props.WorkloadSelector) > 0 {
		if err := validateSelector(props.WorkloadSelector); err != nil {
			return nil, err
		}
	}

	now := clock.Now()

	a := &Application{
		Root:             root,
		organizationID:   props.OrganizationID,
		workspaceID:      props.WorkspaceID,
		projectID:        props.ProjectID,
		ownerTeamID:      props.OwnerTeamID,
		slug:             s,
		displayName:      props.DisplayName,
		sourceID:         optionalString(props.SourceID),
		workloadSelector: copyLabels(props.WorkloadSelector),
		createdAt:        timeOr(props.CreatedAt, now),
		updatedAt:        timeOr(props.UpdatedAt, now),
		archivedAt:       clock.CopyTime(props.ArchivedAt),
	}

	if len(a.workloadSelector) == 0 {
		a.workloadSelector = a.defaultSelector()
	}

	if a.IsNew() {
		a.RecordEvent(ApplicationCreated{a.Snapshot()})
	}

	return a, nil
}

func (a *Application) defaultSelector() map[string]string {
	return map[string]string{WorkloadSelectorLabel: a.ID()}
}

// validateSelector checks a label selector loosely: a bounded number of
// labels, Kubernetes' length limits, and no whitespace.
func validateSelector(selector map[string]string) error {
	if len(selector) > maxSelectorLabels {
		return NewErrInvalidInput(fmt.Sprintf("a workload selector can have at most %d labels", maxSelectorLabels))
	}
	for k, v := range selector {
		if k == "" || len(k) > maxSelectorKeyLength || strings.ContainsAny(k, " \t\n=,") {
			return NewErrInvalidInput(fmt.Sprintf("workload selector key %q must be 1-%d characters without spaces, = or ,", k, maxSelectorKeyLength))
		}
		if len(v) > maxLabelValueLength || strings.ContainsAny(v, " \t\n=,") {
			return NewErrInvalidInput(fmt.Sprintf("workload selector value %q must be at most %d characters without spaces, = or ,", v, maxLabelValueLength))
		}
	}
	return nil
}

func (Application) Prefix() string                         { return "app" }
func (a *Application) OrganizationID() string              { return a.organizationID }
func (a *Application) WorkspaceID() string                 { return a.workspaceID }
func (a *Application) ProjectID() string                   { return a.projectID }
func (a *Application) OwnerTeamID() string                 { return a.ownerTeamID }
func (a *Application) Slug() string                        { return a.slug }
func (a *Application) DisplayName() string                 { return a.displayName }
func (a *Application) SourceID() *string                   { return copyString(a.sourceID) }
func (a *Application) WorkloadSelector() map[string]string { return copyLabels(a.workloadSelector) }
func (a *Application) CreatedAt() time.Time                { return a.createdAt }
func (a *Application) UpdatedAt() time.Time                { return a.updatedAt }
func (a *Application) ArchivedAt() *time.Time              { return clock.CopyTime(a.archivedAt) }

func (a *Application) Archived() bool { return a.archivedAt != nil }

func (a *Application) Snapshot() ApplicationSnapshot {
	return ApplicationSnapshot{
		ID:               a.ID(),
		OrganizationID:   a.organizationID,
		WorkspaceID:      a.workspaceID,
		ProjectID:        a.projectID,
		OwnerTeamID:      a.ownerTeamID,
		Slug:             a.slug,
		DisplayName:      a.displayName,
		SourceID:         copyString(a.sourceID),
		WorkloadSelector: copyLabels(a.workloadSelector),
		CreatedAt:        a.createdAt,
		UpdatedAt:        a.updatedAt,
		ArchivedAt:       clock.CopyTime(a.archivedAt),
	}
}

func (a *Application) checkActive() error {
	if a.Archived() {
		return fmt.Errorf("%w: application %s", ErrArchived, a.ID())
	}
	return nil
}

// Update changes an active Application. Nothing changes if any field is invalid.
func (a *Application) Update(props UpdateApplicationProps) error {
	if err := a.checkActive(); err != nil {
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

	if len(props.WorkloadSelector) > 0 {
		if err := validateSelector(props.WorkloadSelector); err != nil {
			return err
		}
	}

	changed := anyTrue(
		set(&a.slug, props.Slug),
		set(&a.displayName, props.DisplayName),
	)
	if props.WorkloadSelector != nil {
		selector := copyLabels(props.WorkloadSelector)
		if len(selector) == 0 {
			selector = a.defaultSelector()
		}
		if !maps.Equal(selector, a.workloadSelector) {
			a.workloadSelector = selector
			changed = true
		}
	}
	a.updatedAt = clock.Now()

	if changed {
		a.RecordEvent(ApplicationUpdated{a.Snapshot()})
	}
	return nil
}

// LinkSource records the Source the Application is usually built from.
func (a *Application) LinkSource(sourceID string) error {
	if err := a.checkActive(); err != nil {
		return err
	}
	if sourceID == "" {
		return NewErrInvalidInput("a source ID is required; use UnlinkSource to remove it")
	}
	changed := a.sourceID == nil || *a.sourceID != sourceID
	a.sourceID = &sourceID
	a.updatedAt = clock.Now()
	if changed {
		a.RecordEvent(ApplicationSourceLinked{a.Snapshot()})
	}
	return nil
}

// UnlinkSource removes the Application's Source.
func (a *Application) UnlinkSource() error {
	if err := a.checkActive(); err != nil {
		return err
	}
	changed := a.sourceID != nil
	a.sourceID = nil
	a.updatedAt = clock.Now()
	if changed {
		a.RecordEvent(ApplicationSourceUnlinked{a.Snapshot()})
	}
	return nil
}

// TransferTo hands the Application to another Team. The caller checks that
// the Team is active.
func (a *Application) TransferTo(ownerTeamID string) error {
	if err := a.checkActive(); err != nil {
		return err
	}
	if ownerTeamID == "" {
		return NewErrInvalidInput("an application needs an owning team")
	}
	previous := a.ownerTeamID
	a.ownerTeamID = ownerTeamID
	a.updatedAt = clock.Now()
	if ownerTeamID != previous {
		a.RecordEvent(ApplicationTransferred{Application: a.Snapshot(), PreviousOwnerTeamID: previous})
	}
	return nil
}

// Archive archives the Application. Archiving an archived one does nothing.
func (a *Application) Archive() {
	if !a.Archived() {
		now := clock.Now()
		a.archivedAt = &now
		a.updatedAt = now
		a.RecordEvent(ApplicationArchived{a.Snapshot()})
	}
}

// Restore undoes Archive. Restoring an active Application does nothing.
func (a *Application) Restore() {
	if a.Archived() {
		a.archivedAt = nil
		a.updatedAt = clock.Now()
		a.RecordEvent(ApplicationRestored{a.Snapshot()})
	}
}
