package workspace

// DefaultWorkspaceSlug and DefaultWorkspaceName are given to the Workspace
// provisioned with a new Organization. Both can be changed later.
const (
	DefaultWorkspaceSlug = "default"
	DefaultWorkspaceName = "Default"
)

const (
	maxSlugLength        = 63
	maxDisplayNameLength = 255
)

// Permissions this capability asks the authorizer about. The full catalog,
// and which roles include each one, belongs to IAM.
const (
	PermissionWorkspaceRead   = "workspace.read"
	PermissionWorkspaceManage = "workspace.manage"
	PermissionProjectRead     = "project.read"
	PermissionProjectManage   = "project.manage"
	PermissionSettingRead     = "setting.read"
)

// Actions name what an actor asks the authorizer to allow. Policy rules
// and approval requests match on them, and they appear in its decisions.
const (
	ActionProjectArchive      = "project.archive"
	ActionProjectCreate       = "project.create"
	ActionProjectList         = "project.list"
	ActionProjectRead         = "project.read"
	ActionProjectRestore      = "project.restore"
	ActionProjectTransfer     = "project.transfer"
	ActionProjectUpdate       = "project.update"
	ActionSettingChange       = "setting.change"
	ActionSettingList         = "setting.list"
	ActionSettingRemove       = "setting.remove"
	ActionWorkspaceArchive    = "workspace.archive"
	ActionWorkspaceCreate     = "workspace.create"
	ActionWorkspaceList       = "workspace.list"
	ActionWorkspaceRead       = "workspace.read"
	ActionWorkspaceRestore    = "workspace.restore"
	ActionWorkspaceSetDefault = "workspace.set_default"
	ActionWorkspaceUpdate     = "workspace.update"
)

// Audit event types that this capability emits. They are used for recording
// changes and actions within the workspace and project contexts.
const (
	EventWorkspaceCreated      = "workspace.created"
	EventWorkspaceUpdated      = "workspace.updated"
	EventWorkspaceDefaultMoved = "workspace.default_moved"
	EventWorkspaceArchived     = "workspace.archived"
	EventWorkspaceRestored     = "workspace.restored"
	EventProjectCreated        = "project.created"
	EventProjectUpdated        = "project.updated"
	EventProjectOwnerChanged   = "project.owner_changed"
	EventProjectArchived       = "project.archived"
	EventProjectRestored       = "project.restored"
	EventSettingChanged        = "setting.changed"
	EventSettingRemoved        = "setting.removed"
)
