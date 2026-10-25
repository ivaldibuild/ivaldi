package workspace

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
)

// Project is a body of work in a Workspace. Its Workspace never changes.
type Project struct {
	aggregate.Root
	organizationID       string
	workspaceID          string
	ownerTeamID          string
	slug                 string
	displayName          string
	description          string
	createdByPrincipalID string
	createdAt            time.Time
	updatedAt            time.Time
	archivedAt           *time.Time
}

// ProjectSnapshot is a Project's state as plain values, for repositories and
// audit records. Changing it does not change the Project.
type ProjectSnapshot struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	WorkspaceID    string `json:"workspace_id"`
	// OwnerTeamID is the Team responsible for the Project and the default
	// owner of its new Applications. Ownership grants no access (ADR 0007).
	OwnerTeamID          string     `json:"owner_team_id"`
	Slug                 string     `json:"slug"`
	DisplayName          string     `json:"display_name"`
	Description          string     `json:"description"`
	CreatedByPrincipalID string     `json:"created_by_principal_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ArchivedAt           *time.Time `json:"archived_at"`
}

// NewProjectProps describes a Project. A new one leaves the timestamps nil;
// a repository loading a stored one sets them.
type NewProjectProps struct {
	OrganizationID       string
	WorkspaceID          string
	OwnerTeamID          string
	Slug                 string
	DisplayName          string
	Description          string
	CreatedByPrincipalID string
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	ArchivedAt           *time.Time
}

// UpdateProjectProps changes a Project. Nil fields are left as they are. A
// Project's Workspace never changes.
type UpdateProjectProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// NewProject returns a Project, with a new ID unless projectID gives a
// stored one, after checking that it names its Organization, Workspace, and
// owning Team, and has a valid slug and display name. Whether the Team is
// active is checked by the caller.
func NewProject(p NewProjectProps, projectID *string) (*Project, error) {
	if p.OrganizationID == "" || p.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: organization and workspace IDs are required", ErrInvalid)
	}
	root, err := newRoot(Project{}.Prefix(), projectID)
	if err != nil {
		return nil, err
	}
	if p.OwnerTeamID == "" {
		return nil, fmt.Errorf("%w: a project needs an owning team", ErrInvalid)
	}
	if err := ValidateSlug(p.Slug); err != nil {
		return nil, err
	}
	if err := validateDisplayName(p.DisplayName); err != nil {
		return nil, err
	}

	now := now()

	pr := &Project{
		Root:                 root,
		organizationID:       p.OrganizationID,
		workspaceID:          p.WorkspaceID,
		ownerTeamID:          p.OwnerTeamID,
		slug:                 p.Slug,
		displayName:          p.DisplayName,
		description:          p.Description,
		createdByPrincipalID: p.CreatedByPrincipalID,
		createdAt:            timeOr(p.CreatedAt, now),
		updatedAt:            timeOr(p.UpdatedAt, now),
		archivedAt:           copyTime(p.ArchivedAt),
	}
	if pr.IsNew() {
		pr.RecordEvent(ProjectCreated{pr.Snapshot()})
	}
	return pr, nil
}

// Prefix is the type prefix of Project IDs.
func (Project) Prefix() string { return "prj" }

func (p *Project) OrganizationID() string       { return p.organizationID }
func (p *Project) WorkspaceID() string          { return p.workspaceID }
func (p *Project) OwnerTeamID() string          { return p.ownerTeamID }
func (p *Project) Slug() string                 { return p.slug }
func (p *Project) DisplayName() string          { return p.displayName }
func (p *Project) Description() string          { return p.description }
func (p *Project) CreatedByPrincipalID() string { return p.createdByPrincipalID }
func (p *Project) CreatedAt() time.Time         { return p.createdAt }
func (p *Project) UpdatedAt() time.Time         { return p.updatedAt }
func (p *Project) ArchivedAt() *time.Time       { return copyTime(p.archivedAt) }

// Archived reports whether the Project is archived.
func (p *Project) Archived() bool { return p.archivedAt != nil }

// Snapshot returns the Project's state.
func (p *Project) Snapshot() ProjectSnapshot {
	return ProjectSnapshot{
		ID:                   p.ID(),
		OrganizationID:       p.organizationID,
		WorkspaceID:          p.workspaceID,
		OwnerTeamID:          p.ownerTeamID,
		Slug:                 p.slug,
		DisplayName:          p.displayName,
		Description:          p.description,
		CreatedByPrincipalID: p.createdByPrincipalID,
		CreatedAt:            p.createdAt,
		UpdatedAt:            p.updatedAt,
		ArchivedAt:           copyTime(p.archivedAt),
	}
}

// Validate checks the fields that are set.
func (p UpdateProjectProps) Validate() error {
	if p.Slug != nil {
		if err := ValidateSlug(*p.Slug); err != nil {
			return err
		}
	}
	if p.DisplayName != nil {
		if err := validateDisplayName(*p.DisplayName); err != nil {
			return err
		}
	}
	return nil
}

// Update renames or redescribes an active Project.
func (p *Project) Update(u UpdateProjectProps) error {
	if p.Archived() {
		return fmt.Errorf("update project %s: %w", p.ID(), ErrArchived)
	}
	if err := u.Validate(); err != nil {
		return err
	}
	changed := anyTrue(
		set(&p.slug, u.Slug),
		set(&p.displayName, u.DisplayName),
		set(&p.description, u.Description),
	)
	p.updatedAt = now()
	if changed {
		p.RecordEvent(ProjectUpdated{p.Snapshot()})
	}
	return nil
}

// TransferTo hands an active Project to another Team. Whether the Team is
// active is checked by the caller.
func (p *Project) TransferTo(ownerTeamID string) error {
	if p.Archived() {
		return fmt.Errorf("transfer project %s: %w", p.ID(), ErrArchived)
	}
	if ownerTeamID == "" {
		return fmt.Errorf("%w: a project needs an owning team", ErrInvalid)
	}
	previous := p.ownerTeamID
	p.ownerTeamID = ownerTeamID
	p.updatedAt = now()
	if ownerTeamID != previous {
		p.RecordEvent(ProjectTransferred{Project: p.Snapshot(), PreviousOwnerTeamID: previous})
	}
	return nil
}

// Archive archives the Project. Archiving an archived Project does nothing.
// Checking that it has no active dependents is the caller's job.
func (p *Project) Archive() {
	if p.Archived() {
		return
	}
	t := now()
	p.archivedAt = &t
	p.updatedAt = t
	p.RecordEvent(ProjectArchived{p.Snapshot()})
}

// Restore restores an archived Project. Restoring an active one does nothing.
// Checking that its Workspace is active is the caller's job.
func (p *Project) Restore() {
	if !p.Archived() {
		return
	}
	p.archivedAt = nil
	p.updatedAt = now()
	p.RecordEvent(ProjectRestored{p.Snapshot()})
}
