package workspace

// Events a Project records. Each carries the Project as it is after the
// change; the audit record keeps the state before it.

// ProjectCreated is recorded when a Project is created.
type ProjectCreated struct {
	Project ProjectSnapshot `json:"project"`
}

// ProjectUpdated is recorded when a Project's slug, display name, or
// description changes.
type ProjectUpdated struct {
	Project ProjectSnapshot `json:"project"`
}

// ProjectTransferred is recorded when a Project moves to another Team.
type ProjectTransferred struct {
	Project             ProjectSnapshot `json:"project"`
	PreviousOwnerTeamID string          `json:"previous_owner_team_id"`
}

// ProjectArchived is recorded when a Project is archived.
type ProjectArchived struct {
	Project ProjectSnapshot `json:"project"`
}

// ProjectRestored is recorded when an archived Project is restored.
type ProjectRestored struct {
	Project ProjectSnapshot `json:"project"`
}

func (ProjectCreated) EventName() string     { return "workspace.v1.project.created" }
func (ProjectUpdated) EventName() string     { return "workspace.v1.project.updated" }
func (ProjectTransferred) EventName() string { return "workspace.v1.project.transferred" }
func (ProjectArchived) EventName() string    { return "workspace.v1.project.archived" }
func (ProjectRestored) EventName() string    { return "workspace.v1.project.restored" }
