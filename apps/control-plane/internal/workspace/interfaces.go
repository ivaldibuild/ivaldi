package workspace

// The interfaces this capability defines for itself: its repository, the
// hooks other capabilities implement, and its services. Contracts shared
// with other capabilities live with their owners: identity.Actor and the
// authorizer in pkg/, and IAM resources, audit, and Teams in their
// control-plane capability packages.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Repository persists and retrieves Workspaces, Projects, and settings, and
// nothing more: IDs, timestamps, and every rule come from the domain. Every
// method is scoped to an Organization. Methods return ErrNotFound when no row matches,
// ErrSlugTaken for a slug clash, and ErrConflict for a lost concurrent update.
type Repository interface {
	CreateWorkspace(ctx context.Context, w *Workspace) error
	GetWorkspace(ctx context.Context, organizationID, workspaceID string) (*Workspace, error)
	// GetWorkspaceForUpdate is GetWorkspace that also locks the row until the
	// transaction ends. It must be called inside a transaction.
	GetWorkspaceForUpdate(ctx context.Context, organizationID, workspaceID string) (*Workspace, error)
	GetDefaultWorkspace(ctx context.Context, organizationID string) (*Workspace, error)
	ListWorkspaces(ctx context.Context, organizationID string, includeArchived bool) ([]*Workspace, error)
	// UpdateWorkspace writes the Workspace's slug, display name, description,
	// default flag, archived time, and updated time.
	UpdateWorkspace(ctx context.Context, w *Workspace) error
	CountActiveProjects(ctx context.Context, organizationID, workspaceID string) (int, error)
	// CountActiveProjectsOwnedBy counts the active Projects a Team owns.
	CountActiveProjectsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error)

	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, organizationID, projectID string) (*Project, error)
	ListProjects(ctx context.Context, organizationID, workspaceID string, includeArchived bool) ([]*Project, error)
	// UpdateProject writes the Project's slug, display name, description,
	// owning Team, archived time, and updated time. It never changes WorkspaceID.
	UpdateProject(ctx context.Context, p *Project) error

	GetSetting(ctx context.Context, scope Scope, key string) (*Setting, error)
	ListSettings(ctx context.Context, scope Scope) ([]*Setting, error)
	// SettingsForResolution returns the values stored for key at the scope and
	// every level above it: at most one each for the Organization, Workspace, and Project.
	SettingsForResolution(ctx context.Context, scope Scope, key string) ([]*Setting, error)
	// InsertSetting stores a value at a level that has none. It returns
	// ErrConflict if one was stored concurrently.
	InsertSetting(ctx context.Context, s *Setting) error
	// UpdateSetting replaces the value if its stored version is expectedVersion,
	// and increments the version. It returns ErrConflict otherwise.
	UpdateSetting(ctx context.Context, s *Setting, expectedVersion int64) error
	// DeleteSetting removes the value if its stored version is expectedVersion.
	// It returns ErrConflict otherwise.
	DeleteSetting(ctx context.Context, scope Scope, key string, expectedVersion int64) error
}

// LockSource reports which setting keys are locked, as Organization and
// Workspace policy rules owned by the policy capability.
type LockSource interface {
	// LockedKeys returns the level each locked key is locked at, for the
	// Organization and, if workspaceID is set, that Workspace. When both lock a
	// key, it reports LevelOrganization, because an Organization lock wins.
	LockedKeys(ctx context.Context, organizationID, workspaceID string) (map[string]Level, error)
}

// NoLocks returns a LockSource that reports no locked keys. Nothing is
// locked until the policy capability exists and provides the real source.
func NoLocks() LockSource { return noLocks{} }

type noLocks struct{}

func (noLocks) LockedKeys(context.Context, string, string) (map[string]Level, error) {
	return map[string]Level{}, nil
}

// ArchiveTarget is the Workspace, or Project when ProjectID is set, being archived.
type ArchiveTarget struct {
	OrganizationID string
	WorkspaceID    string
	ProjectID      string
}

// Dependency is something still active under an ArchiveTarget.
type Dependency struct {
	Capability string
	// Kind names what is active, such as "Application".
	Kind  string
	Count int
}

func (d Dependency) String() string {
	return fmt.Sprintf("%d active %s (%s)", d.Count, d.Kind, d.Capability)
}

// Dependent is implemented by every capability that keeps records under a
// Workspace or Project. Archiving is refused while any reports active records.
type Dependent interface {
	ActiveDependents(ctx context.Context, target ArchiveTarget) ([]Dependency, error)
}

// Each service interface below is implemented by one sub-package, which
// also serves it over HTTP: workspaces for WorkspaceService, projects for
// ProjectService, and settings for SettingService. A sub-package's
// constructor returns its service wrapped in an authorizing layer that
// checks every operation with the single authorizer before it runs, so the
// interface is the only way in. Other capabilities read through Lookups and
// SettingResolver, which do not authorize.

// WorkspaceService manages an Organization's Workspaces.
type WorkspaceService interface {
	CreateWorkspace(ctx context.Context, actor identity.Actor, in CreateWorkspaceInput) (*Workspace, error)
	GetWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error)
	// ListWorkspaces needs read access at Organization level; listing only
	// the Workspaces each caller can see waits for IAM's resource listing.
	ListWorkspaces(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*Workspace, error)
	// UpdateWorkspace renames or redescribes an active Workspace, including the default one.
	UpdateWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, props UpdateWorkspaceProps) (*Workspace, error)
	// SetDefaultWorkspace makes an active Workspace the Organization's
	// default, in place of the current one.
	SetDefaultWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error)
	// ArchiveWorkspace archives a Workspace that is not the default and has
	// no active Projects or other dependents. Archiving an archived one does nothing.
	ArchiveWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error)
	// RestoreWorkspace restores an archived Workspace if its slug is still
	// free. Restoring an active one does nothing.
	RestoreWorkspace(ctx context.Context, actor identity.Actor, organizationID, workspaceID string) (*Workspace, error)
}

// CreateWorkspaceInput describes a new Workspace.
type CreateWorkspaceInput struct {
	OrganizationID string
	Slug           string
	DisplayName    string
	Description    string
}

// Validate checks the input without reading anything, so a request that can
// never succeed is refused before the authorizer is asked.
func (in CreateWorkspaceInput) Validate() error {
	if in.OrganizationID == "" {
		return fmt.Errorf("%w: organization ID is required", ErrInvalid)
	}
	if err := ValidateSlug(in.Slug); err != nil {
		return err
	}
	return validateDisplayName(in.DisplayName)
}

// ProjectService manages the Projects in Workspaces.
type ProjectService interface {
	// CreateProject creates a Project in an active Workspace, owned by an active Team.
	CreateProject(ctx context.Context, actor identity.Actor, in CreateProjectInput) (*Project, error)
	GetProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*Project, error)
	// ListProjects needs read access on the Workspace; listing only the
	// Projects each caller can see waits for IAM's resource listing.
	ListProjects(ctx context.Context, actor identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*Project, error)
	// UpdateProject renames or redescribes an active Project.
	UpdateProject(ctx context.Context, actor identity.Actor, organizationID, projectID string, props UpdateProjectProps) (*Project, error)
	// TransferProject hands an active Project to another active Team. The
	// Project's existing Applications keep their own owners.
	TransferProject(ctx context.Context, actor identity.Actor, organizationID, projectID, ownerTeamID string) (*Project, error)
	// ArchiveProject archives a Project with no active dependents. Archiving
	// an archived one does nothing.
	ArchiveProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*Project, error)
	// RestoreProject restores an archived Project in an active Workspace, if
	// its slug is still free. Restoring an active one does nothing.
	RestoreProject(ctx context.Context, actor identity.Actor, organizationID, projectID string) (*Project, error)
}

// CreateProjectInput describes a new Project.
type CreateProjectInput struct {
	OrganizationID string
	WorkspaceID    string
	OwnerTeamID    string
	Slug           string
	DisplayName    string
	Description    string
}

// Validate checks the input without reading anything, so a request that can
// never succeed is refused before the authorizer is asked. Whether the Team
// is active is checked later.
func (in CreateProjectInput) Validate() error {
	if in.OrganizationID == "" || in.WorkspaceID == "" {
		return fmt.Errorf("%w: organization and workspace IDs are required", ErrInvalid)
	}
	if in.OwnerTeamID == "" {
		return fmt.Errorf("%w: a project needs an owning team", ErrInvalid)
	}
	if err := ValidateSlug(in.Slug); err != nil {
		return err
	}
	return validateDisplayName(in.DisplayName)
}

// SettingService changes and reads the settings stored for Organizations,
// Workspaces, and Projects on behalf of an actor.
type SettingService interface {
	// ChangeSetting sets a value at one level. An agent Session's change
	// always needs a human's approval (ADR 0007): the first call returns an
	// authorization.ApprovalRequiredError, and the change applies when
	// resubmitted with that decision's ID after approval.
	ChangeSetting(ctx context.Context, actor identity.Actor, in ChangeSettingInput) (*Setting, error)
	// RemoveSetting removes a value at one level, so the value from the next
	// level up applies again. It is authorized like a change.
	RemoveSetting(ctx context.Context, actor identity.Actor, in RemoveSettingInput) error
	// ListSettings returns the values stored at exactly one level.
	ListSettings(ctx context.Context, actor identity.Actor, scope Scope) ([]*Setting, error)
	// ResolveSetting returns a setting's effective value for a scope, as
	// SettingResolver does, for an actor allowed to read the scope's settings.
	ResolveSetting(ctx context.Context, actor identity.Actor, scope Scope, key string) (Resolved, error)
}

// ChangeSettingInput proposes a setting value at one level.
type ChangeSettingInput struct {
	Scope Scope
	Key   string
	Value json.RawMessage
	// ExpectedVersion is the version the caller read: 0 when the level had no
	// value. The change is refused if the stored version differs.
	ExpectedVersion int64
	// ApprovalID resubmits a change that required approval, once approved.
	ApprovalID string
}

// RemoveSettingInput removes the value stored at one level, so the value
// from the next level up applies again.
type RemoveSettingInput struct {
	Scope           Scope
	Key             string
	ExpectedVersion int64
	ApprovalID      string
}
