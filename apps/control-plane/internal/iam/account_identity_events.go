package iam

// Events an AccountIdentity records. Sign-ins record no event: they are
// contact, not changes to the link.

// AccountIdentityCreated is recorded when an Account is linked to a subject a
// platform provider asserts.
type AccountIdentityCreated struct {
	AccountIdentity AccountIdentitySnapshot `json:"account_identity"`
}

func (AccountIdentityCreated) EventName() string { return "iam.v1.account_identity.created" }
