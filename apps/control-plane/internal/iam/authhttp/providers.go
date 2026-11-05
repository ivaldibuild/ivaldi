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

// providerFlowTTL bounds a sign-in at an identity provider.
const providerFlowTTL = 10 * time.Minute

// oauthCookie holds a sign-in in progress at an identity provider.
const oauthCookie = "ivaldi-oauth"

// Provider is an identity provider people can sign in with: a connector and
// the Organization identity provider it signs in to.
type Provider struct {
	// Kind names it in its URLs, such as /auth/oauth/github/start.
	Kind               string
	Connector          connector.Connector
	OrganizationID     string
	IdentityProviderID string
}

// WithProviders adds the identity providers shown on the login page.
func WithProviders(p ...Provider) Option {
	return func(h *handlers) { h.providers = append(h.providers, p...) }
}

// providerFlow is what the oauth cookie holds between leaving for the
// provider and coming back.
type providerFlow struct {
	Kind     string `json:"k"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
}

func (h *handlers) registerProviderHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/oauth/{kind}/start", h.startProvider)
	mux.HandleFunc("GET /auth/oauth/{kind}/callback", h.providerCallback)
}

func (h *handlers) provider(kind string) (Provider, bool) {
	for _, p := range h.providers {
		if p.Kind == kind {
			return p, true
		}
	}
	return Provider{}, false
}

func (h *handlers) providerLinks(back string) []ProviderLink {
	links := make([]ProviderLink, 0, len(h.providers))
	for _, p := range h.providers {
		start := "/auth/oauth/" + p.Kind + "/start"
		if back != "" {
			start += "?" + url.Values{"return_to": {back}}.Encode()
		}
		links = append(links, ProviderLink{Name: p.Connector.DisplayName(), StartURL: start})
	}
	return links
}

func (h *handlers) callbackURL(kind string) string {
	return h.settings.Issuer + "/auth/oauth/" + kind + "/callback"
}

func (h *handlers) startProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := h.provider(r.PathValue("kind"))
	if !ok {
		h.renderError(w, http.StatusNotFound, "That sign-in method is not available.")
		return
	}
	back, _ := returnTo(r.URL.Query().Get("return_to"))
	flow := providerFlow{Kind: p.Kind, State: rand.Text(), Nonce: rand.Text(), Verifier: pkceVerifier(), ReturnTo: back}
	sealed, err := h.codec.Encode(oauthCookie, flow, providerFlowTTL)
	if err != nil {
		h.serverError(w, err)
		return
	}
	location, err := p.Connector.AuthCodeURL(r.Context(), connector.AuthRequest{
		State: flow.State, Nonce: flow.Nonce, Verifier: flow.Verifier, RedirectURI: h.callbackURL(p.Kind),
	})
	if err != nil {
		h.logger.Error("start provider sign-in", logging.F("provider", p.Kind), logging.F("error", err))
		h.renderError(w, http.StatusBadGateway, p.Connector.DisplayName()+" cannot be reached right now. Try again later.")
		return
	}
	h.setCookie(w, oauthCookie, sealed, providerFlowTTL)
	http.Redirect(w, r, location, http.StatusFound)
}

func (h *handlers) providerCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := h.provider(r.PathValue("kind"))
	if !ok {
		h.renderError(w, http.StatusNotFound, "That sign-in method is not available.")
		return
	}
	var flow providerFlow
	found := h.readCookie(r, oauthCookie, &flow)
	h.clearCookie(w, oauthCookie)
	q := r.URL.Query()
	// The state ties the callback to the browser that started it, so no one
	// can sign someone else in with their own account at the provider.
	if !found || flow.Kind != p.Kind || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(flow.State)) != 1 {
		h.renderError(w, http.StatusBadRequest, "This sign-in has expired or was started in another browser. Start again.")
		return
	}
	if q.Get("error") != "" {
		h.renderError(w, http.StatusBadRequest, "Signing in with "+p.Connector.DisplayName()+" was cancelled.")
		return
	}
	if q.Get("code") == "" {
		h.renderError(w, http.StatusBadRequest, p.Connector.DisplayName()+" sent no sign-in code.")
		return
	}

	profile, err := p.Connector.Exchange(r.Context(), connector.Callback{
		Code: q.Get("code"), Verifier: flow.Verifier, Nonce: flow.Nonce, RedirectURI: h.callbackURL(p.Kind),
	})
	if err != nil {
		h.logger.Warn("provider sign-in failed", logging.F("provider", p.Kind), logging.F("error", err))
		h.renderError(w, http.StatusBadGateway, p.Connector.DisplayName()+" could not confirm who you are. Start again.")
		return
	}
	signedIn, err := h.auth.AuthenticateExternal(r.Context(), p.OrganizationID, p.IdentityProviderID, profile)
	if err != nil {
		h.externalSignInError(w, p, err)
		return
	}
	if err := h.startSignIn(w, signIn{
		AccountID:          signedIn.AccountID,
		Method:             signedIn.Method,
		IdentityProviderID: signedIn.IdentityProviderID,
		RestrictedTo:       signedIn.RestrictedTo,
	}); err != nil {
		h.serverError(w, err)
		return
	}
	h.continueTo(w, r, flow.ReturnTo)
}

func (h *handlers) externalSignInError(w http.ResponseWriter, p Provider, err error) {
	name := p.Connector.DisplayName()
	switch {
	case errors.Is(err, iam.ErrNotAdmitted):
		h.renderError(w, http.StatusForbidden, "Your "+name+" account is not allowed to sign in here.")
	case errors.Is(err, iam.ErrNoMembership):
		h.renderError(w, http.StatusForbidden, "You are not a member here yet. Ask an administrator to add you.")
	case errors.Is(err, iam.ErrAccountExists):
		h.renderError(w, http.StatusConflict, "An account with your email already exists. Sign in with it another way first.")
	case errors.Is(err, iam.ErrStatus):
		h.renderError(w, http.StatusForbidden, "This account cannot sign in. Ask an administrator.")
	default:
		h.serverError(w, err)
	}
}

// pkceVerifier returns a PKCE code verifier: 32 random bytes, base64url.
func pkceVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
