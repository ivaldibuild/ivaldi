package workspace

// The interfaces this capability defines for itself. Contracts shared with
// other capabilities live with their owners: identity.Actor and the
// authorizer in pkg/, and IAM resources, audit, and Teams in their
// control-plane capability packages.

import (
	"context"
	"fmt"
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
