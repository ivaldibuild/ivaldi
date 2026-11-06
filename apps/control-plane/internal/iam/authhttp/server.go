package authhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// Cookies the authorization server sets on its own origin, before the
// __Host- prefix they carry over https.
const (
	// signInCookie remembers who signed in, so a later authorization
	// request skips the frontend's sign-in.
	signInCookie = "ivaldi-signin"
	// loginCookie binds a login challenge to the browser that started it,
	// so only that browser can finish it.
	loginCookie = "ivaldi-login"
	// oauthCookie holds a sign-in in progress at an identity provider.
	oauthCookie = "ivaldi-oauth"
)

// maxFormBytes bounds a form post to the token and revocation endpoints.
const maxFormBytes = 16 << 10

// Settings configure the authorization server's HTTP side.
type Settings struct {
	// Issuer is the control plane's external URL; cookies are Secure when it
	// uses https.
	Issuer string
	// LoginURL is the frontend's sign-in page.
	LoginURL string
	// SessionTTL is how long a browser stays signed in to the authorization
	// server, and how long a login challenge lasts.
	SessionTTL time.Duration
	// BuiltinAccounts tells the frontend to offer email and password.
	BuiltinAccounts bool
}

// Option configures New.
type Option func(*handlers)

func WithSettings(s Settings) Option { return func(h *handlers) { h.settings = s } }
func WithLogger(l logging.Logger) Option {
	return func(h *handlers) { h.logger = l }
}
func WithAuthentication(a iam.Authentication) Option { return func(h *handlers) { h.auth = a } }
func WithAuthorizationServer(s iam.AuthorizationServer) Option {
	return func(h *handlers) { h.oauth = s }
}
func WithCodec(c session.Codec) Option { return func(h *handlers) { h.codec = c } }

// WithOnboarding lets people sign up for the managed cloud with a platform
// provider. Without it, a platform sign-in with no Account is refused.
func WithOnboarding(o iam.Onboarding) Option { return func(h *handlers) { h.onboarding = o } }

// Server is the authorization server's HTTP side: plain handlers for the
// browser's redirects and OAuth's form posts, and JSON operations under /v1
// for the frontend's sign-in screens.
type Server interface {
	RegisterHandlers(mux *http.ServeMux)
	RegisterRoutes(api huma.API)
}

type handlers struct {
	settings   Settings
	logger     logging.Logger
	auth       iam.Authentication
	oauth      iam.AuthorizationServer
	codec      session.Codec
	onboarding iam.Onboarding
	providers  []Provider
	secure     bool
}

// New returns the authorization server's HTTP side. It serves no pages: the
// frontend at Settings.LoginURL renders sign-in.
func New(opts ...Option) (Server, error) {
	h := &handlers{}
	for _, o := range opts {
		o(h)
	}
	switch {
	case h.logger == nil, h.auth == nil, h.oauth == nil, h.codec == nil:
		return nil, errors.New("authhttp: logger, authentication, authorization server, and codec are required")
	case h.settings.SessionTTL <= 0:
		return nil, errors.New("authhttp: session TTL must be positive")
	}
	issuer, err := url.Parse(h.settings.Issuer)
	if err != nil || issuer.Host == "" {
		return nil, fmt.Errorf("authhttp: issuer %q is not an absolute URL", h.settings.Issuer)
	}
	if login, err := url.Parse(h.settings.LoginURL); err != nil || login.Host == "" {
		return nil, fmt.Errorf("authhttp: login URL %q is not an absolute URL", h.settings.LoginURL)
	}
	h.secure = issuer.Scheme == "https"
	return h, nil
}

func (h *handlers) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET "+iam.AuthorizationServerMetadataPath, h.metadata)
	mux.HandleFunc("GET "+iam.OAuthAuthorizePath, h.authorize)
	mux.HandleFunc("GET "+ContinuePath, h.continueAuthorization)
	mux.HandleFunc("POST "+iam.OAuthTokenPath, h.token)
	mux.HandleFunc("POST "+iam.OAuthRevokePath, h.revoke)
	mux.HandleFunc("GET "+LogoutPath, h.logout)
	mux.HandleFunc("GET "+providerStartPattern, h.startProvider)
	mux.HandleFunc("GET "+providerCallbackPattern, h.providerCallback)
}

// signIn is what the sign-in cookie holds: who signed in, and how.
type signIn struct {
	AccountID          string           `json:"acc"`
	Method             iam.SignInMethod `json:"m"`
	IdentityProviderID string           `json:"idp,omitempty"`
	RestrictedTo       string           `json:"org,omitempty"`
}

func (s signIn) allows(organizationID string) bool {
	return s.RestrictedTo == "" || s.RestrictedTo == organizationID
}

func signInOf(s *iam.SignedIn) signIn {
	return signIn{AccountID: s.AccountID, Method: s.Method, IdentityProviderID: s.IdentityProviderID, RestrictedTo: s.RestrictedTo}
}

func (h *handlers) cookieName(name string) string {
	// __Host- cookies must be Secure, host-only, and on path /, so no other
	// host or path can set or shadow them.
	if h.secure {
		return "__Host-" + name
	}
	return name
}

func (h *handlers) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName(name),
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl / time.Second),
		Secure:   h.secure,
		HttpOnly: true,
		// Lax keeps the cookies on top-level navigations back from the
		// frontend and from identity providers, and off cross-site posts.
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *handlers) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(name), Path: "/", MaxAge: -1, Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// sealCookie seals v into the named cookie.
func (h *handlers) sealCookie(w http.ResponseWriter, name string, v any, ttl time.Duration) error {
	value, err := h.codec.Encode(name, v, ttl)
	if err != nil {
		return err
	}
	h.setCookie(w, name, value, ttl)
	return nil
}

func (h *handlers) readCookie(r *http.Request, name string, v any) bool {
	c, err := r.Cookie(h.cookieName(name))
	if err != nil {
		return false
	}
	return h.codec.Decode(name, c.Value, v) == nil
}

func (h *handlers) currentSignIn(r *http.Request) (signIn, bool) {
	var s signIn
	ok := h.readCookie(r, signInCookie, &s)
	return s, ok && s.AccountID != ""
}

// redirect sends the browser to location. Unlike http.Redirect, it writes no
// HTML body: the control plane serves no pages.
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusFound)
}

// toFrontend redirects the browser to the frontend's sign-in page with q.
func (h *handlers) toFrontend(w http.ResponseWriter, r *http.Request, q url.Values) {
	redirect(w, iam.AppendQuery(h.settings.LoginURL, q))
}

// failToFrontend sends the browser to the frontend with an error code it
// can explain, keeping the challenge so the person can try again.
func (h *handlers) failToFrontend(w http.ResponseWriter, r *http.Request, challenge, code string) {
	q := url.Values{"error": {code}}
	if challenge != "" {
		q.Set("login_challenge", challenge)
	}
	h.toFrontend(w, r, q)
}

func (h *handlers) serverError(w http.ResponseWriter, r *http.Request, challenge string, err error) {
	h.logger.Error("sign-in failed", logging.F("error", err))
	h.failToFrontend(w, r, challenge, errorServer)
}

// Routes returns the sign-in JSON operations with nothing to serve them,
// for describing the API: registering them reads none of a server's
// dependencies. Serving them needs New.
func Routes() interface{ RegisterRoutes(api huma.API) } { return &handlers{} }
