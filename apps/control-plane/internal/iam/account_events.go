package iam

// Events an Account records. Each carries the Account as it is after the
// change; the audit record keeps the state before it.

// AccountCreated is recorded when an Account is created.
type AccountCreated struct {
	Account AccountSnapshot `json:"account"`
}

// AccountRenamed is recorded when an Account's display name changes.
type AccountRenamed struct {
	Account AccountSnapshot `json:"account"`
}

// AccountEmailChanged is recorded when an Account's email changes. A change
// beyond its case leaves the email unverified.
type AccountEmailChanged struct {
	Account AccountSnapshot `json:"account"`
}

// AccountEmailVerified is recorded when an Account's holder proves they own
// its email.
type AccountEmailVerified struct {
	Account AccountSnapshot `json:"account"`
}

// AccountStatusChanged is recorded when an Account is suspended,
// deactivated, or reactivated.
type AccountStatusChanged struct {
	Account        AccountSnapshot `json:"account"`
	PreviousStatus AccountStatus   `json:"previous_status"`
}

func (AccountCreated) EventName() string       { return "iam.v1.account.created" }
func (AccountRenamed) EventName() string       { return "iam.v1.account.renamed" }
func (AccountEmailChanged) EventName() string  { return "iam.v1.account.email_changed" }
func (AccountEmailVerified) EventName() string { return "iam.v1.account.email_verified" }
func (AccountStatusChanged) EventName() string { return "iam.v1.account.status_changed" }
