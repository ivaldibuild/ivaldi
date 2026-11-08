package authhttp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// Where the browser goes to sign in with an identity provider, and where
// the provider sends it back. The callback is the redirect URI to register
// with the provider.
const (
	providerStartPattern    = "/auth/oauth/{kind}/start"
	providerCallbackPattern = "/auth/oauth/{kind}/callback"
)

// providerFlowTTL bounds a sign-in at an identity provider.
const providerFlowTTL = 10 * time.Minute

// Provider is an identity provider people can sign in with: a connector and
// either the Organization identity provider it signs in to, or, in the
// managed cloud, the platform provider it is.
type Provider struct {
	// Kind names it in its URLs, such as /auth/oauth/github/start.
	Kind               string
	Connector          connector.Connector
	OrganizationID     string
	IdentityProviderID string
	// Platform, when set, makes it a platform provider: it signs in an
	// Account rather than a User, and can sign people up.
	Platform iam.PlatformProvider
}

// WithProviders adds the identity providers the frontend offers.
func WithProviders(p ...Provider) Option {
	return func(h *handlers) { h.providers = append(h.providers, p...) }
}

// WithInstallReturns names, by provider kind, the frontend page that
// finishes installing that provider's app, such as GitHub's at
// <web app>/settings/connections/github/callback.
//
// A GitHub App that both signs people in and connects accounts sends a
// person back from an install to its first callback URL, which may be this
// sign-in callback, and GitHub's settings do not let the order be chosen.
// The callback recognises such a return and sends it on, so either order
// works.
func WithInstallReturns(pages map[string]string) Option {
	return func(h *handlers) { h.installReturns = pages }
}

// isInstallReturn reports whether a callback is the provider returning from
// an app install rather than from a sign-in: only an install carries an
// installation ID and a setup action.
func isInstallReturn(q url.Values) bool {
	return q.Get("installation_id") != "" && q.Get("setup_action") != ""
}

// providerFlow is what the oauth cookie holds between leaving for the
// provider and coming back.
type providerFlow struct {
	Kind      string `json:"k"`
	State     string `json:"s"`
	Nonce     string `json:"n"`
	Verifier  string `json:"v"`
	Challenge string `json:"c"`
}

func (h *handlers) provider(kind string) (Provider, bool) {
	for _, p := range h.providers {
		if p.Kind == kind {
			return p, true
		}
	}
	return Provider{}, false
}

func (h *handlers) startURL(kind, challenge string) string {
	return h.settings.Issuer + "/auth/oauth/" + kind + "/start?" + url.Values{"login_challenge": {challenge}}.Encode()
}

func (h *handlers) callbackURL(kind string) string {
	return h.settings.Issuer + "/auth/oauth/" + kind + "/callback"
}

// startProvider sends the browser to the identity provider. The frontend
// links here from its sign-in page with the login challenge.
func (h *handlers) startProvider(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if _, err := h.openChallenge(challenge); err != nil {
		h.failToFrontend(w, r, "", errorExpired)
		return
	}
	p, ok := h.provider(r.PathValue("kind"))
	if !ok {
		h.failToFrontend(w, r, challenge, errorNotAdmitted)
		return
	}
	flow := providerFlow{Kind: p.Kind, State: rand.Text(), Nonce: rand.Text(), Verifier: pkceVerifier(), Challenge: challenge}
	location, err := p.Connector.AuthCodeURL(r.Context(), connector.AuthRequest{
		State: flow.State, Nonce: flow.Nonce, Verifier: flow.Verifier, RedirectURI: h.callbackURL(p.Kind),
	})
	if err != nil {
		h.logger.Error("start provider sign-in", logging.F("provider", p.Kind), logging.F("error", err))
		h.failToFrontend(w, r, challenge, errorProvider)
		return
	}
	if err := h.sealCookie(w, oauthCookie, flow, providerFlowTTL); err != nil {
		h.serverError(w, r, challenge, err)
		return
	}
	redirect(w, location)
}

// providerCallback finishes a sign-in at the identity provider, then
// continues the authorization, or sends the browser back to the frontend to
// choose an Organization, sign up, or show why it failed.
func (h *handlers) providerCallback(w http.ResponseWriter, r *http.Request) {
	// An install return goes on to the frontend unchanged, before the
	// sign-in cookie is read or cleared, so a sign-in in progress in another
	// tab is left alone. The frontend completes it with the person's own
	// token, and the control plane checks its state there.
	if page, ok := h.installReturns[r.PathValue("kind")]; ok && isInstallReturn(r.URL.Query()) {
		redirect(w, iam.AppendQuery(page, r.URL.Query()))
		return
	}
	var flow providerFlow
	found := h.readCookie(r, oauthCookie, &flow)
	h.clearCookie(w, oauthCookie)
	q := r.URL.Query()
	// The state ties the callback to the browser that started it, so no one
	// can sign someone else in with their own account at the provider.
	if !found || flow.Kind != r.PathValue("kind") || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(flow.State)) != 1 {
		h.failToFrontend(w, r, "", errorExpired)
		return
	}
	ch, err := h.openChallenge(flow.Challenge)
	if err != nil {
		h.failToFrontend(w, r, "", errorExpired)
		return
	}
	p, ok := h.provider(flow.Kind)
	if !ok {
		h.failToFrontend(w, r, flow.Challenge, errorNotAdmitted)
		return
	}
	if q.Get("error") != "" {
		h.failToFrontend(w, r, flow.Challenge, errorCancelled)
		return
	}
	profile, err := p.Connector.Exchange(r.Context(), connector.Callback{
		Code: q.Get("code"), Verifier: flow.Verifier, Nonce: flow.Nonce, RedirectURI: h.callbackURL(p.Kind),
	})
	if err != nil {
		h.logger.Warn("provider sign-in failed", logging.F("provider", p.Kind), logging.F("error", err))
		h.failToFrontend(w, r, flow.Challenge, errorProvider)
		return
	}

	var signedIn *iam.SignedIn
	if p.Platform != "" {
		signedIn, err = h.auth.AuthenticatePlatform(r.Context(), p.Platform, profile)
		if errors.Is(err, iam.ErrSignUpRequired) && h.onboarding != nil {
			token, err := h.codec.Encode(signUpName, signUpToken{
				Nonce: ch.Nonce, Provider: p.Platform, Subject: profile.Subject, Email: profile.Email,
				EmailVerified: profile.EmailVerified, DisplayName: profile.DisplayName,
			}, loginFlowTTL)
			if err != nil {
				h.serverError(w, r, flow.Challenge, err)
				return
			}
			h.toFrontend(w, r, url.Values{"login_challenge": {flow.Challenge}, "signup_token": {token}, "email": {profile.Email}})
			return
		}
		if errors.Is(err, iam.ErrSignUpRequired) {
			err = iam.ErrNoMembership
		}
	} else {
		signedIn, err = h.auth.AuthenticateExternal(r.Context(), p.OrganizationID, p.IdentityProviderID, profile)
	}
	if err != nil {
		h.signInFailed(w, r, flow.Challenge, err)
		return
	}

	step, err := h.next(r.Context(), ch, signInOf(signedIn), "")
	if err != nil {
		h.signInFailed(w, r, flow.Challenge, err)
		return
	}
	if step.RedirectTo != "" {
		redirect(w, step.RedirectTo)
		return
	}
	h.toFrontend(w, r, url.Values{"login_challenge": {flow.Challenge}, "login_session": {step.LoginSession}})
}

func (h *handlers) signInFailed(w http.ResponseWriter, r *http.Request, challenge string, err error) {
	code := errorCode(err)
	if code == errorServer {
		h.serverError(w, r, challenge, err)
		return
	}
	h.failToFrontend(w, r, challenge, code)
}

// pkceVerifier returns a PKCE code verifier: 32 random bytes, base64url.
func pkceVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
