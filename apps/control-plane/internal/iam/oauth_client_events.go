package iam

// Events an OAuthClient records. Each carries the OAuthClient as it is after
// the change; the audit record keeps the state before it. Uses record no
// event: they are contact, not changes to the client.

// OAuthClientCreated is recorded when an OAuthClient registers.
type OAuthClientCreated struct {
	OAuthClient OAuthClientSnapshot `json:"oauth_client"`
}

// OAuthClientUpdated is recorded when an OAuthClient's name or redirect URIs
// change.
type OAuthClientUpdated struct {
	OAuthClient OAuthClientSnapshot `json:"oauth_client"`
}

func (OAuthClientCreated) EventName() string { return "iam.v1.oauth_client.created" }
func (OAuthClientUpdated) EventName() string { return "iam.v1.oauth_client.updated" }
