package iam

// Events an ExternalIdentity records. Sign-ins record no event: they are
// contact, not changes to the link.

// ExternalIdentityCreated is recorded when a User is linked to a subject an
// identity provider asserts.
type ExternalIdentityCreated struct {
	ExternalIdentity ExternalIdentitySnapshot `json:"external_identity"`
}

func (ExternalIdentityCreated) EventName() string { return "iam.v1.external_identity.created" }
