package iam

import (
	"context"
	"net/url"
)

// Where the authorization server's endpoints are, under the issuer URL.
const (
	OAuthAuthorizePath = "/oauth/authorize"
	OAuthTokenPath     = "/oauth/token"
	OAuthRevokePath    = "/oauth/revoke"
	JWKSPath           = "/.well-known/jwks.json"
	// AuthorizationServerMetadataPath serves RFC 8414's document.
	AuthorizationServerMetadataPath = "/.well-known/oauth-authorization-server"
)

// OAuth error codes (RFC 6749, RFC 8707, OpenID Connect's login_required).
const (
	OAuthInvalidRequest          = "invalid_request"
	OAuthInvalidClient           = "invalid_client"
	OAuthInvalidGrant            = "invalid_grant"
	OAuthUnauthorizedClient      = "unauthorized_client"
	OAuthUnsupportedGrantType    = "unsupported_grant_type"
	OAuthUnsupportedResponseType = "unsupported_response_type"
	OAuthInvalidScope            = "invalid_scope"
	OAuthInvalidTarget           = "invalid_target"
	OAuthAccessDenied            = "access_denied"
	OAuthLoginRequired           = "login_required"
	OAuthServerError             = "server_error"
)

// AuthorizationServer is the control plane's OAuth 2.1 authorization
// server: authorization code with PKCE, and rotating refresh tokens.
// Authentication decides who the person is; this decides what the client
// gets.
type AuthorizationServer interface {
	// ValidateAuthorization checks an authorization request before anyone
	// signs in. Its errors are *OAuthError; one with a RedirectURI goes back
	// to the client, and one without is shown to the person, because the
	// client or its redirect URI cannot be trusted.
	ValidateAuthorization(ctx context.Context, req AuthorizeRequest) (*ValidatedAuthorization, error)
	// IssueCode issues an authorization code for the membership the person
	// chose, and returns the URL to send their browser back to the client.
	IssueCode(ctx context.Context, v *ValidatedAuthorization, m Membership) (string, error)
	// Exchange answers a token request: a code or a refresh token for an
	// access token and a new refresh token. Its errors are *OAuthError.
	Exchange(ctx context.Context, req TokenRequest) (*TokenResponse, error)
	// Revoke revokes a refresh token and the rest of its family (RFC 7009).
	// An unknown token, or an access token, is not an error.
	Revoke(ctx context.Context, token, clientID string) error
	// Metadata describes the server (RFC 8414).
	Metadata() AuthorizationServerMetadata
}

// AuthorizeRequest is an authorization request's parameters.
type AuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	Scope               string
}

// ValidatedAuthorization is an authorization request that passed every
// check, waiting for the person to sign in.
type ValidatedAuthorization struct {
	// Client is the client's record ID; ClientID its public client_id.
	Client        string
	ClientID      string
	ClientName    string
	RedirectURI   string
	State         string
	CodeChallenge string
	Resource      string
	Scopes        []string
}

// TokenRequest is a token request's parameters. Public clients identify
// themselves with ClientID; PKCE or the refresh token binds the grant to them.
type TokenRequest struct {
	GrantType    string
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Resource     string
	Scope        string
}

// TokenResponse is a successful token response (RFC 6749 section 5.1).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope,omitempty"`
}

// AuthorizationServerMetadata is RFC 8414's document.
type AuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint"`
	JWKSURI                                    string   `json:"jwks_uri"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported"`
	ScopesSupported                            []string `json:"scopes_supported,omitempty"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

// OAuthError is an error the OAuth protocol reports to the client.
type OAuthError struct {
	Code        string
	Description string
	// RedirectURI is where the error goes when the client and its redirect
	// URI were verified; empty means it must not be redirected anywhere.
	RedirectURI string
	State       string
}

func (e *OAuthError) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return e.Code + ": " + e.Description
}

// Location returns the URL that carries the error back to the client, with
// the issuer so the client can tell which server answered (RFC 9207).
func (e *OAuthError) Location(issuer string) string {
	q := url.Values{"error": {e.Code}, "iss": {issuer}}
	if e.Description != "" {
		q.Set("error_description", e.Description)
	}
	if e.State != "" {
		q.Set("state", e.State)
	}
	return AppendQuery(e.RedirectURI, q)
}

// AppendQuery adds parameters to a URL that may already have a query.
func AppendQuery(raw string, q url.Values) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	existing := u.Query()
	for k, vs := range q {
		for _, v := range vs {
			existing.Add(k, v)
		}
	}
	u.RawQuery = existing.Encode()
	return u.String()
}
