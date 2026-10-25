package iam

// Events a GroupMember records.

// GroupMemberAdded is recorded when a principal joins a Group.
type GroupMemberAdded struct {
	GroupMember GroupMemberSnapshot `json:"group_member"`
}

func (GroupMemberAdded) EventName() string { return "iam.v1.group_member.added" }
