package iam

// Events an OAuthAuthorizationCode records. It carries the code without its
// hash. Issuing a code records no event: a code has no ID or stored creation
// time that tells a new one from a stored one, and the tokens it is redeemed
// for record their own.

// OAuthAuthorizationCodeRedeemed is recorded when a client exchanges an
// authorization code.
type OAuthAuthorizationCodeRedeemed struct {
	OAuthAuthorizationCode OAuthAuthorizationCodeSnapshot `json:"oauth_authorization_code"`
}

func (OAuthAuthorizationCodeRedeemed) EventName() string {
	return "iam.v1.oauth_authorization_code.redeemed"
}
