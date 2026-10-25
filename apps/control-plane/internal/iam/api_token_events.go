package iam

// Events an APIToken records. Each carries the APIToken as it is after the
// change, without its hash. Uses record no event: they are contact, not
// changes to the token.

// APITokenCreated is recorded when an APIToken is issued.
type APITokenCreated struct {
	APIToken APITokenSnapshot `json:"api_token"`
}

// APITokenRevoked is recorded when an APIToken is revoked.
type APITokenRevoked struct {
	APIToken APITokenSnapshot `json:"api_token"`
}

func (APITokenCreated) EventName() string { return "iam.v1.api_token.created" }
func (APITokenRevoked) EventName() string { return "iam.v1.api_token.revoked" }
