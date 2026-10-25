package iam

// Events a Role records. Each carries the Role as it is after the change;
// the audit record keeps the state before it.

// RoleCreated is recorded when a Role is created.
type RoleCreated struct {
	Role RoleSnapshot `json:"role"`
}

// RoleRenamed is recorded when a custom Role's name changes.
type RoleRenamed struct {
	Role RoleSnapshot `json:"role"`
}

func (RoleCreated) EventName() string { return "iam.v1.role.created" }
func (RoleRenamed) EventName() string { return "iam.v1.role.renamed" }
