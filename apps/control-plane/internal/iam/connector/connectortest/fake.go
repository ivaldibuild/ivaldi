// Package connectortest provides a Connector for tests that need no
// identity provider.
package connectortest

import (
	"context"
	"net/url"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector"
)

// Fake signs in whoever Profile says, and records the requests it gets.
type Fake struct {
	Kind    iam.IdentityProtocol
	Name    string
	Profile iam.ExternalProfile
	// Err, when set, is what Exchange returns.
	Err error

	Requests  []connector.AuthRequest
	Callbacks []connector.Callback
}

var _ connector.Connector = (*Fake)(nil)

func (f *Fake) Protocol() iam.IdentityProtocol { return f.Kind }
func (f *Fake) DisplayName() string            { return f.Name }

// AuthCodeURL points at a fake provider page carrying the state, so a test
// can play the provider by calling the callback with it.
func (f *Fake) AuthCodeURL(_ context.Context, req connector.AuthRequest) (string, error) {
	f.Requests = append(f.Requests, req)
	return "https://idp.example.com/authorize?" + url.Values{"state": {req.State}, "redirect_uri": {req.RedirectURI}}.Encode(), nil
}

func (f *Fake) Exchange(_ context.Context, cb connector.Callback) (iam.ExternalProfile, error) {
	f.Callbacks = append(f.Callbacks, cb)
	if f.Err != nil {
		return iam.ExternalProfile{}, f.Err
	}
	return f.Profile, nil
}
