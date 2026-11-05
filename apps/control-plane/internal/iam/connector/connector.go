// Package connector signs people in with upstream identity providers:
// Google, GitHub, and any OpenID Connect provider. Each sits behind the
// Ivaldi-owned Connector interface, so no provider's library or wire format
// reaches the rest of IAM; a connector only turns a provider's answer into
// an iam.ExternalProfile.
package connector

import (
	"context"
	"errors"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// ErrExchange means the provider refused the sign-in or answered with
// something the connector cannot trust, such as an ID token with the wrong
// nonce.
var ErrExchange = errors.New("connector: sign-in with the provider failed")

// AuthRequest starts a sign-in at the provider.
type AuthRequest struct {
	// State is echoed back to the callback, tying it to this browser.
	State string
	// Nonce is bound into an OpenID Connect ID token.
	Nonce string
	// Verifier is the PKCE code verifier; the provider gets its S256 challenge.
	Verifier string
	// RedirectURI is the control plane's callback for this provider.
	RedirectURI string
}

// Callback finishes a sign-in with what the provider sent back.
type Callback struct {
	Code        string
	Verifier    string
	Nonce       string
	RedirectURI string
}

// Connector signs people in with one identity provider.
type Connector interface {
	Protocol() iam.IdentityProtocol
	// DisplayName labels the provider's sign-in button.
	DisplayName() string
	// AuthCodeURL returns where to send the browser to sign in.
	AuthCodeURL(ctx context.Context, req AuthRequest) (string, error)
	// Exchange redeems the provider's code and returns who signed in, or an
	// error matching ErrExchange.
	Exchange(ctx context.Context, cb Callback) (iam.ExternalProfile, error)
}
