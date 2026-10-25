package iam

// Events an OAuthRefreshToken records. Each carries the OAuthRefreshToken as
// it is after the change, without its hash.

// OAuthRefreshTokenCreated is recorded when a refresh token is issued,
// including as the replacement for a rotated one.
type OAuthRefreshTokenCreated struct {
	OAuthRefreshToken OAuthRefreshTokenSnapshot `json:"oauth_refresh_token"`
}

// OAuthRefreshTokenRotated is recorded when a refresh token is used and
// replaced; its snapshot names the replacement.
type OAuthRefreshTokenRotated struct {
	OAuthRefreshToken OAuthRefreshTokenSnapshot `json:"oauth_refresh_token"`
}

// OAuthRefreshTokenRevoked is recorded when a refresh token is revoked.
type OAuthRefreshTokenRevoked struct {
	OAuthRefreshToken OAuthRefreshTokenSnapshot `json:"oauth_refresh_token"`
}

func (OAuthRefreshTokenCreated) EventName() string { return "iam.v1.oauth_refresh_token.created" }
func (OAuthRefreshTokenRotated) EventName() string { return "iam.v1.oauth_refresh_token.rotated" }
func (OAuthRefreshTokenRevoked) EventName() string { return "iam.v1.oauth_refresh_token.revoked" }
