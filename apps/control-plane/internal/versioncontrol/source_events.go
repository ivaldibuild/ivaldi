package versioncontrol

// SourceCreated is recorded when a repository is connected under a Project.
type SourceCreated struct {
	Source SourceSnapshot `json:"source"`
}

// SourceUpdated is recorded when a Source's repository metadata changes,
// such as after a rename or a default-branch change.
type SourceUpdated struct {
	Source SourceSnapshot `json:"source"`
}

// SourceArchived is recorded when a Source is disconnected.
type SourceArchived struct {
	Source SourceSnapshot `json:"source"`
}

// SourceRestored is recorded when a disconnected Source is connected again.
type SourceRestored struct {
	Source SourceSnapshot `json:"source"`
}

func (SourceCreated) EventName() string  { return "versioncontrol.v1.source.created" }
func (SourceUpdated) EventName() string  { return "versioncontrol.v1.source.updated" }
func (SourceArchived) EventName() string { return "versioncontrol.v1.source.archived" }
func (SourceRestored) EventName() string { return "versioncontrol.v1.source.restored" }
