package authhttp

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// Paths of the sign-in pages.
const (
	LoginPath         = "/auth/login"
	OrganizationsPath = "/auth/organizations"
	LogoutPath        = "/auth/logout"
)

// Cookie names, before the __Host- prefix they carry over https.
const (
	signInCookie = "ivaldi-signin"
	csrfCookie   = "ivaldi-csrf"
)

// csrfTTL outlives a sign-in session, so a login page left open for a while
// still submits.
const csrfTTL = time.Hour

// maxFormBytes bounds a form post.
const maxFormBytes = 16 << 10

// Settings configure the handlers.
type Settings struct {
	// Issuer is the control plane's external URL; cookies are Secure, and
	// form posts must come from its origin.
	Issuer string
	// SessionTTL is how long a browser stays signed in to the authorization
	// server.
	SessionTTL time.Duration
	// BuiltinAccounts shows the email and password form.
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

type handlers struct {
	settings   Settings
	logger     logging.Logger
	auth       iam.Authentication
	oauth      iam.AuthorizationServer
	codec      session.Codec
	onboarding iam.Onboarding
	providers  []Provider
	origin     string
	secure     bool
}

// New returns the authorization server's handlers: its metadata, the OAuth
// endpoints, and the sign-in pages.
func New(opts ...Option) (api.Handlers, error) {
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
	u, err := url.Parse(h.settings.Issuer)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("authhttp: issuer %q is not an absolute URL", h.settings.Issuer)
	}
	h.origin = u.Scheme + "://" + u.Host
	h.secure = u.Scheme == "https"
	return h, nil
}

func (h *handlers) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET "+iam.AuthorizationServerMetadataPath, h.metadata)
	mux.HandleFunc("GET "+iam.OAuthAuthorizePath, h.authorize)
	mux.HandleFunc("POST "+iam.OAuthTokenPath, h.token)
	mux.HandleFunc("POST "+iam.OAuthRevokePath, h.revoke)
	mux.HandleFunc("GET "+LoginPath, h.loginPage)
	mux.HandleFunc("POST "+LoginPath, h.login)
	mux.HandleFunc("GET "+OrganizationsPath, h.organizationsPage)
	mux.HandleFunc("POST "+OrganizationsPath, h.chooseOrganization)
	mux.HandleFunc("POST "+LogoutPath, h.logout)
	h.registerProviderHandlers(mux)
	h.registerSignUpHandlers(mux)
}

// signIn is what the sign-in cookie holds.
type signIn struct {
	AccountID          string           `json:"acc"`
	Method             iam.SignInMethod `json:"m"`
	IdentityProviderID string           `json:"idp,omitempty"`
	RestrictedTo       string           `json:"org,omitempty"`
}

func (s signIn) allows(organizationID string) bool {
	return s.RestrictedTo == "" || s.RestrictedTo == organizationID
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
		// Lax keeps the cookie on the top-level navigation a client starts
		// to /oauth/authorize, and off cross-site posts.
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *handlers) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(name), Path: "/", MaxAge: -1, Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
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

func (h *handlers) startSignIn(w http.ResponseWriter, s signIn) error {
	value, err := h.codec.Encode(signInCookie, s, h.settings.SessionTTL)
	if err != nil {
		return err
	}
	h.setCookie(w, signInCookie, value, h.settings.SessionTTL)
	return nil
}

// csrfToken returns the browser's CSRF token, setting one if it has none.
// Forms echo it, and posts must match the cookie: a page elsewhere can make
// the browser post, but cannot read the cookie to fill in the token.
func (h *handlers) csrfToken(w http.ResponseWriter, r *http.Request) (string, error) {
	var token string
	if h.readCookie(r, csrfCookie, &token) && token != "" {
		return token, nil
	}
	token = rand.Text()
	value, err := h.codec.Encode(csrfCookie, token, csrfTTL)
	if err != nil {
		return "", err
	}
	h.setCookie(w, csrfCookie, value, csrfTTL)
	return token, nil
}

// checkPost parses a form post and checks it came from the sign-in pages:
// from this origin, with the browser's CSRF token.
func (h *handlers) checkPost(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "The form could not be read.")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != h.origin {
		h.renderError(w, http.StatusForbidden, "This form was posted from another site.")
		return false
	}
	var want string
	got := r.PostForm.Get("csrf")
	if !h.readCookie(r, csrfCookie, &want) || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		h.renderError(w, http.StatusForbidden, "This form has expired. Go back, reload the page, and try again.")
		return false
	}
	return true
}

// returnTo returns a sign-in page's destination if it is the authorization
// endpoint on this server, so the pages cannot be used to redirect anywhere
// else.
func returnTo(raw string) (string, bool) {
	if raw == "" || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != iam.OAuthAuthorizePath {
		return "", false
	}
	return u.RequestURI(), true
}

// pageHeaders keep the pages out of frames, caches, and other origins.
func pageHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
