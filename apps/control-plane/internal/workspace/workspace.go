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
// persists. Repositories read state through Snapshot and rebuild stored
// objects with the same New functions, passing the stored ID and timestamps;
// they never change an object. Creation rules therefore also run on load, so
// a rule added later must still accept existing rows.
package workspace

import (
	"fmt"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
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
	aggregate.Root
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

// WorkspaceSnapshot is a Workspace's state as plain values, for repositories and
// audit records. Changing it does not change the Workspace.
type WorkspaceSnapshot struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Slug           string `json:"slug"`
	DisplayName    string `json:"display_name"`
	Description    string `json:"description"`
	// IsDefault marks the Organization's default Workspace. There is exactly
	// one, and it cannot be archived.
	IsDefault bool `json:"is_default"`
	// CreatedByPrincipalID is empty for the default Workspace, which the system creates.
	CreatedByPrincipalID string     `json:"created_by_principal_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ArchivedAt           *time.Time `json:"archived_at"`
}

// NewWorkspaceProps describes a Workspace. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewWorkspaceProps struct {
	OrganizationID       string
	Slug                 string
	DisplayName          string
	Description          string
	IsDefault            bool
	CreatedByPrincipalID string
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	ArchivedAt           *time.Time
}

// NewWorkspace returns a Workspace, with a new ID unless workspaceID gives a
// stored one, after checking its organization, slug, and display name.
func NewWorkspace(p NewWorkspaceProps, workspaceID *string) (*Workspace, error) {
	if p.OrganizationID == "" {
		return nil, fmt.Errorf("%w: organization ID is required", ErrInvalid)
	}

	root, err := newRoot(Workspace{}.Prefix(), workspaceID)
	if err != nil {
		return nil, err
	}

	if err := ValidateSlug(p.Slug); err != nil {
		return nil, err
	}

	if err := validateDisplayName(p.DisplayName); err != nil {
		return nil, err
	}

	now := now()

	w := &Workspace{
		Root:                 root,
		organizationID:       p.OrganizationID,
		slug:                 p.Slug,
		displayName:          p.DisplayName,
		description:          p.Description,
		isDefault:            p.IsDefault,
		createdByPrincipalID: p.CreatedByPrincipalID,
		createdAt:            timeOr(p.CreatedAt, now),
		updatedAt:            timeOr(p.UpdatedAt, now),
		archivedAt:           copyTime(p.ArchivedAt),
	}

	if w.IsNew() {
		w.RecordEvent(WorkspaceCreated{w.Snapshot()})
	}

	return w, nil
}

// NewDefaultWorkspace creates the default Workspace for a new Organization.
func NewDefaultWorkspace(organizationID string) (*Workspace, error) {
	return NewWorkspace(NewWorkspaceProps{OrganizationID: organizationID, Slug: DefaultWorkspaceSlug, DisplayName: DefaultWorkspaceName, IsDefault: true}, nil)
}

// Prefix is the type prefix of Workspace IDs.
func (Workspace) Prefix() string { return "wsp" }

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

// Snapshot returns the Workspace's state.
func (w *Workspace) Snapshot() WorkspaceSnapshot {
	return WorkspaceSnapshot{
		ID:                   w.ID(),
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
		return fmt.Errorf("update workspace %s: %w", w.ID(), ErrArchived)
	}

	if err := p.Validate(); err != nil {
		return err
	}

	changed := anyTrue(
		set(&w.slug, p.Slug),
		set(&w.displayName, p.DisplayName),
		set(&w.description, p.Description),
	)

	w.updatedAt = now()

	if changed {
		w.RecordEvent(WorkspaceUpdated{w.Snapshot()})
	}

	return nil
}

// MakeDefault makes an active Workspace the default. The caller clears the
// previous default first, because an Organization has only one.
func (w *Workspace) MakeDefault() error {
	if w.Archived() {
		return fmt.Errorf("make workspace %s the default: %w", w.ID(), ErrArchived)
	}

	changed := !w.isDefault
	w.isDefault = true
	w.updatedAt = now()

	if changed {
		w.RecordEvent(WorkspaceMadeDefault{w.Snapshot()})
	}

	return nil
}

// ClearDefault stops the Workspace being the default, when another becomes the default.
func (w *Workspace) ClearDefault() {
	changed := w.isDefault
	w.isDefault = false
	w.updatedAt = now()

	if changed {
		w.RecordEvent(WorkspaceDefaultCleared{w.Snapshot()})
	}
}

// Archive archives the Workspace. The default Workspace cannot be archived;
// archiving an archived Workspace does nothing. Checking that it has no active
// Projects or other dependents is the caller's job, because those live outside it.
func (w *Workspace) Archive() error {
	if w.Archived() {
		return nil
	}

	if w.isDefault {
		return fmt.Errorf("archive workspace %s: %w; make another workspace the default first", w.ID(), ErrDefaultWorkspace)
	}

	t := now()
	w.archivedAt = &t
	w.updatedAt = t
	w.RecordEvent(WorkspaceArchived{w.Snapshot()})

	return nil
}

// Restore restores an archived Workspace. Restoring an active one does nothing.
func (w *Workspace) Restore() {
	if !w.Archived() {
		return
	}

	w.archivedAt = nil
	w.updatedAt = now()
	w.RecordEvent(WorkspaceRestored{w.Snapshot()})
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

// set assigns *v to *dst when v is given and differs, and reports whether it
// changed anything, so a method records an event only for a real change.
func set[T comparable](dst *T, v *T) bool {
	if v == nil || *dst == *v {
		return false
	}
	*dst = *v
	return true
}

// anyTrue reports whether any of changes is true. Every argument is
// evaluated, so it combines several set calls without short-circuiting.
func anyTrue(changes ...bool) bool {
	return slices.Contains(changes, true)
}

// timeOr returns a stored timestamp, or fallback for a new object.
func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}

// newRoot returns an aggregate's identity: storedID, a stored row's ID given
// to a New constructor, which must have the type's prefix, or a new ID.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return r, nil
}
