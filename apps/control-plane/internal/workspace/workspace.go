// Package workspace owns Workspaces, Projects, and the settings stored for
// Organizations, Workspaces, and Projects.
//
// See docs/design/workspace-data-model.md. This package holds the domain types,
// the repository interface, and the service with the capability's rules. The
// PostgreSQL implementation lives in the postgres subpackage; this package
// never imports a database driver (ADR 0005).
//
// Domain objects are the only way to change domain state. Their fields are
// unexported, they are created through New functions that apply the creation
// rules, and they change only through their methods, which enforce the rest.
// The service orchestrates: it authorizes, loads, calls those methods, and
// persists. Repositories read state through Data and rebuild stored objects
// with the Reconstitute functions; they never change an object.
package workspace

import (
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Domain types whose IDs carry a prefix register it, so a prefix used twice
// fails when the control plane starts.
func init() {
	id.MustRegister(Workspace{}.Prefix(), "workspace.Workspace")
	id.MustRegister(Project{}.Prefix(), "workspace.Project")
}

// Workspace is a space in an Organization where related work happens.
type Workspace struct {
	id                   string
	organizationID       string
	slug                 string
	displayName          string
	description          string
	isDefault            bool
	createdByPrincipalID string
	createdAt            time.Time
	updatedAt            time.Time
	archivedAt           *time.Time
}

// WorkspaceData is a Workspace's state as plain values, for repositories and
// audit records. Changing it does not change the Workspace.
type WorkspaceData struct {
	ID             string
	OrganizationID string
	Slug           string
	DisplayName    string
	Description    string
	// IsDefault marks the Organization's default Workspace. There is exactly
	// one, and it cannot be archived.
	IsDefault bool
	// CreatedByPrincipalID is empty for the default Workspace, which the system creates.
	CreatedByPrincipalID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ArchivedAt           *time.Time
}

// NewWorkspaceProps describes a new Workspace.
type NewWorkspaceProps struct {
	OrganizationID       string
	Slug                 string
	DisplayName          string
	Description          string
	CreatedByPrincipalID string
}

// NewWorkspace creates a Workspace with a new ID, after checking its organization, slug, and display name.
func NewWorkspace(p NewWorkspaceProps, workspaceID *string) (*Workspace, error) {
	if p.OrganizationID == "" {
		return nil, fmt.Errorf("%w: organization ID is required", ErrInvalid)
	}

	if err := ValidateSlug(p.Slug); err != nil {
		return nil, err
	}

	if err := validateDisplayName(p.DisplayName); err != nil {
		return nil, err
	}

	now := now()

	w := &Workspace{
		organizationID:       p.OrganizationID,
		slug:                 p.Slug,
		displayName:          p.DisplayName,
		description:          p.Description,
		createdByPrincipalID: p.CreatedByPrincipalID,
		createdAt:            now,
		updatedAt:            now,
	}

	if workspaceID != nil {
		w.id = *workspaceID
	}

	id.Assign(&w.id, w)

	return w, nil
}

// NewDefaultWorkspace creates the default Workspace for a new Organization.
func NewDefaultWorkspace(organizationID string) (*Workspace, error) {
	w, err := NewWorkspace(NewWorkspaceProps{OrganizationID: organizationID, Slug: DefaultWorkspaceSlug, DisplayName: DefaultWorkspaceName}, nil)
	if err != nil {
		return nil, err
	}

	w.isDefault = true

	return w, nil
}

// ReconstituteWorkspace rebuilds a Workspace that is already stored, without
// applying creation rules. It is for repositories only.
func ReconstituteWorkspace(d WorkspaceData) *Workspace {
	return &Workspace{
		id:                   d.ID,
		organizationID:       d.OrganizationID,
		slug:                 d.Slug,
		displayName:          d.DisplayName,
		description:          d.Description,
		isDefault:            d.IsDefault,
		createdByPrincipalID: d.CreatedByPrincipalID,
		createdAt:            d.CreatedAt,
		updatedAt:            d.UpdatedAt,
		archivedAt:           copyTime(d.ArchivedAt),
	}
}

// Prefix is the type prefix of Workspace IDs.
func (Workspace) Prefix() string { return "wsp" }

func (w *Workspace) ID() string                   { return w.id }
func (w *Workspace) OrganizationID() string       { return w.organizationID }
func (w *Workspace) Slug() string                 { return w.slug }
func (w *Workspace) DisplayName() string          { return w.displayName }
func (w *Workspace) Description() string          { return w.description }
func (w *Workspace) IsDefault() bool              { return w.isDefault }
func (w *Workspace) CreatedByPrincipalID() string { return w.createdByPrincipalID }
func (w *Workspace) CreatedAt() time.Time         { return w.createdAt }
func (w *Workspace) UpdatedAt() time.Time         { return w.updatedAt }
func (w *Workspace) ArchivedAt() *time.Time       { return copyTime(w.archivedAt) }

// Archived reports whether the Workspace is archived.
func (w *Workspace) Archived() bool { return w.archivedAt != nil }

// Data returns the Workspace's state.
func (w *Workspace) Data() WorkspaceData {
	return WorkspaceData{
		ID:                   w.id,
		OrganizationID:       w.organizationID,
		Slug:                 w.slug,
		DisplayName:          w.displayName,
		Description:          w.description,
		IsDefault:            w.isDefault,
		CreatedByPrincipalID: w.createdByPrincipalID,
		CreatedAt:            w.createdAt,
		UpdatedAt:            w.updatedAt,
		ArchivedAt:           copyTime(w.archivedAt),
	}
}

// UpdateWorkspaceProps changes a Workspace. Nil fields are left as they are.
type UpdateWorkspaceProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// Validate checks the fields that are set.
func (p UpdateWorkspaceProps) Validate() error {
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

// Update renames or redescribes an active Workspace, including the default one.
func (w *Workspace) Update(p UpdateWorkspaceProps) error {
	if w.Archived() {
		return fmt.Errorf("update workspace %s: %w", w.id, ErrArchived)
	}

	if err := p.Validate(); err != nil {
		return err
	}

	if p.Slug != nil {
		w.slug = *p.Slug
	}

	if p.DisplayName != nil {
		w.displayName = *p.DisplayName
	}

	if p.Description != nil {
		w.description = *p.Description
	}

	w.updatedAt = now()

	return nil
}

// MakeDefault makes an active Workspace the default. The caller clears the
// previous default first, because an Organization has only one.
func (w *Workspace) MakeDefault() error {
	if w.Archived() {
		return fmt.Errorf("make workspace %s the default: %w", w.id, ErrArchived)
	}

	w.isDefault = true
	w.updatedAt = now()

	return nil
}

// ClearDefault stops the Workspace being the default, when another becomes the default.
func (w *Workspace) ClearDefault() {
	w.isDefault = false
	w.updatedAt = now()
}

// Archive archives the Workspace. The default Workspace cannot be archived;
// archiving an archived Workspace does nothing. Checking that it has no active
// Projects or other dependents is the caller's job, because those live outside it.
func (w *Workspace) Archive() error {
	if w.Archived() {
		return nil
	}

	if w.isDefault {
		return fmt.Errorf("archive workspace %s: %w; make another workspace the default first", w.id, ErrDefaultWorkspace)
	}

	t := now()
	w.archivedAt = &t
	w.updatedAt = t

	return nil
}

// Restore restores an archived Workspace. Restoring an active one does nothing.
func (w *Workspace) Restore() {
	if !w.Archived() {
		return
	}

	w.archivedAt = nil
	w.updatedAt = now()
}

var slugFormat = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateSlug checks that s is lowercase letters and digits separated by
// single hyphens, at most 63 characters, so it is safe in URLs.
func ValidateSlug(s string) error {
	if len(s) == 0 || len(s) > maxSlugLength || !slugFormat.MatchString(s) {
		return fmt.Errorf("%w: slug %q must be 1-%d lowercase letters, digits, and single hyphens", ErrInvalid, s, maxSlugLength)
	}
	return nil
}

func validateDisplayName(s string) error {
	if s == "" || utf8.RuneCountInString(s) > maxDisplayNameLength {
		return fmt.Errorf("%w: display name must be 1-%d characters", ErrInvalid, maxDisplayNameLength)
	}
	return nil
}

// copyTime returns a copy of t, so callers cannot change a domain object
// through a pointer it handed out or was given.
func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}
