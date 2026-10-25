package iam

// Events a Group records. Each carries the Group as it is after the change;
// the audit record keeps the state before it. Membership has its own events.

// GroupCreated is recorded when a Group is created.
type GroupCreated struct {
	Group GroupSnapshot `json:"group"`
}

// GroupRenamed is recorded when a Group's name changes.
type GroupRenamed struct {
	Group GroupSnapshot `json:"group"`
}

func (GroupCreated) EventName() string { return "iam.v1.group.created" }
func (GroupRenamed) EventName() string { return "iam.v1.group.renamed" }
