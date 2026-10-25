package iam

// Events a Grant records. A Grant does not change once made.

// GrantCreated is recorded when a Role is granted on a resource.
type GrantCreated struct {
	Grant GrantSnapshot `json:"grant"`
}

func (GrantCreated) EventName() string { return "iam.v1.grant.created" }
