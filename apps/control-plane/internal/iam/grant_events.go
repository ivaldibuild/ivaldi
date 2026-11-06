package iam

// Events a Grant records. A Grant does not change once made; it is
// removed and granted again.

// GrantCreated is recorded when a Role is granted on a resource.
type GrantCreated struct {
	Grant GrantSnapshot `json:"grant"`
}

// GrantRemoved is recorded when a Grant is removed. It carries the Grant as
// it was.
type GrantRemoved struct {
	Grant GrantSnapshot `json:"grant"`
}

func (GrantCreated) EventName() string { return "iam.v1.grant.created" }
func (GrantRemoved) EventName() string { return "iam.v1.grant.removed" }
