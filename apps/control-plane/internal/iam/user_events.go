package iam

// Events an User records. Each carries the User as it is after the change.
// Changes common to every principal are recorded as principal events.

// UserCreated is recorded when an Account becomes a member of an
// Organization.
type UserCreated struct {
	User UserSnapshot `json:"user"`
}

func (UserCreated) EventName() string { return "iam.v1.user.created" }
