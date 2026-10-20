package workspace

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Project is a body of work in a Workspace. Its Workspace never changes.
type Project struct {
	id                   string
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

// ProjectData is a Project's state as plain values, for repositories and
// audit records. Changing it does not change the Project.
type ProjectData struct {
	ID             string
	OrganizationID string
	WorkspaceID    string
	// OwnerTeamID is the Team responsible for the Project and the default
	// owner of its new Applications. Ownership grants no access (ADR 0007).
	OwnerTeamID          string
	Slug                 string
	DisplayName          string
	Description          string
	CreatedByPrincipalID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ArchivedAt           *time.Time
}

// NewProjectProps describes a new Project.
type NewProjectProps struct {
	OrganizationID       string
	WorkspaceID          string
	OwnerTeamID          string
	Slug                 string
	DisplayName          string
	Description          string
	CreatedByPrincipalID string
}

// UpdateProjectProps changes a Project. Nil fields are left as they are. A
// Project's Workspace never changes.
type UpdateProjectProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// NewProject creates a Project with a new ID, after checking that it names
// its Organization, Workspace, and owning Team, and has a valid slug and
// display name. Whether the Team is active is checked by the caller.
func NewProject(p NewProjectProps) (*Project, error) {
	if p.OrganizationID == "" || p.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: organization and workspace IDs are required", ErrInvalid)
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
		organizationID:       p.OrganizationID,
		workspaceID:          p.WorkspaceID,
		ownerTeamID:          p.OwnerTeamID,
		slug:                 p.Slug,
		displayName:          p.DisplayName,
		description:          p.Description,
		createdByPrincipalID: p.CreatedByPrincipalID,
		createdAt:            now,
		updatedAt:            now,
	}
	id.Assign(&pr.id, pr)
	return pr, nil
}

// ReconstituteProject rebuilds a Project that is already stored, without
// applying creation rules. It is for repositories only.
func ReconstituteProject(d ProjectData) *Project {
	return &Project{
		id:                   d.ID,
		organizationID:       d.OrganizationID,
		workspaceID:          d.WorkspaceID,
		ownerTeamID:          d.OwnerTeamID,
		slug:                 d.Slug,
		displayName:          d.DisplayName,
		description:          d.Description,
		createdByPrincipalID: d.CreatedByPrincipalID,
		createdAt:            d.CreatedAt,
		updatedAt:            d.UpdatedAt,
		archivedAt:           copyTime(d.ArchivedAt),
	}
}

// Prefix is the type prefix of Project IDs.
func (Project) Prefix() string { return "prj" }

func (p *Project) ID() string                   { return p.id }
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

// Data returns the Project's state.
func (p *Project) Data() ProjectData {
	return ProjectData{
		ID:                   p.id,
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
		return fmt.Errorf("update project %s: %w", p.id, ErrArchived)
	}
	if err := u.Validate(); err != nil {
		return err
	}
	if u.Slug != nil {
		p.slug = *u.Slug
	}
	if u.DisplayName != nil {
		p.displayName = *u.DisplayName
	}
	if u.Description != nil {
		p.description = *u.Description
	}
	p.updatedAt = now()
	return nil
}

// TransferTo hands an active Project to another Team. Whether the Team is
// active is checked by the caller.
func (p *Project) TransferTo(ownerTeamID string) error {
	if p.Archived() {
		return fmt.Errorf("transfer project %s: %w", p.id, ErrArchived)
	}
	if ownerTeamID == "" {
		return fmt.Errorf("%w: a project needs an owning team", ErrInvalid)
	}
	p.ownerTeamID = ownerTeamID
	p.updatedAt = now()
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
}

// Restore restores an archived Project. Restoring an active one does nothing.
// Checking that its Workspace is active is the caller's job.
func (p *Project) Restore() {
	if !p.Archived() {
		return
	}
	p.archivedAt = nil
	p.updatedAt = now()
}
