package workspace

// Events a Workspace records. Each carries the Workspace as it is after the
// change; the audit record keeps the state before it.

// WorkspaceCreated is recorded when a Workspace is created, including an
// Organization's default one.
type WorkspaceCreated struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

// WorkspaceUpdated is recorded when a Workspace's slug, display name, or
// description changes.
type WorkspaceUpdated struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

// WorkspaceMadeDefault is recorded when a Workspace becomes its
// Organization's default.
type WorkspaceMadeDefault struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

// WorkspaceDefaultCleared is recorded when a Workspace stops being the
// default, because another became it.
type WorkspaceDefaultCleared struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

// WorkspaceArchived is recorded when a Workspace is archived.
type WorkspaceArchived struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

// WorkspaceRestored is recorded when an archived Workspace is restored.
type WorkspaceRestored struct {
	Workspace WorkspaceSnapshot `json:"workspace"`
}

func (WorkspaceCreated) EventName() string        { return "workspace.v1.workspace.created" }
func (WorkspaceUpdated) EventName() string        { return "workspace.v1.workspace.updated" }
func (WorkspaceMadeDefault) EventName() string    { return "workspace.v1.workspace.made_default" }
func (WorkspaceDefaultCleared) EventName() string { return "workspace.v1.workspace.default_cleared" }
func (WorkspaceArchived) EventName() string       { return "workspace.v1.workspace.archived" }
func (WorkspaceRestored) EventName() string       { return "workspace.v1.workspace.restored" }
