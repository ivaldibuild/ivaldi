package authhttp

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
)

// SignUpPath asks a person signing up for their first Organization.
const SignUpPath = "/auth/signup/organization"

// signUpCookie holds a platform sign-in waiting to become a sign-up.
const signUpCookie = "ivaldi-signup"

// signUpTTL bounds how long the Organization form may take.
const signUpTTL = 10 * time.Minute

// WithOnboarding lets people sign up for the managed cloud with a platform
// provider. Without it, a platform sign-in with no Account is refused.
func WithOnboarding(o iam.Onboarding) Option { return func(h *handlers) { h.onboarding = o } }

// pendingSignUp is what the platform provider said, kept until the person
// names their Organization.
type pendingSignUp struct {
	Provider      iam.PlatformProvider `json:"p"`
	Subject       string               `json:"s"`
	Email         string               `json:"e"`
	EmailVerified bool                 `json:"ev"`
	DisplayName   string               `json:"n,omitempty"`
	ReturnTo      string               `json:"r,omitempty"`
}

type signUpData struct {
	Title, Error, CSRF, Email, Name, Slug, SignUpPath string
}

var signUpContent = mustClone(`{{define "content"}}
<h1>Create your organization</h1>
<p>Signing up as {{.Email}}.</p>
{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="{{.SignUpPath}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="name">Organization name</label>
  <input id="name" name="name" required value="{{.Name}}">
  <label for="slug">Address (optional)</label>
  <input id="slug" name="slug" pattern="[a-z0-9-]+" value="{{.Slug}}" placeholder="derived from the name">
  <button type="submit">Create organization</button>
</form>
{{end}}`)

func (h *handlers) registerSignUpHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET "+SignUpPath, h.signUpPage)
	mux.HandleFunc("POST "+SignUpPath, h.signUp)
}

// platformSignIn finishes a sign-in with a platform provider: the person is
// signed in, or, when no Account matches, asked to sign up.
func (h *handlers) platformSignIn(w http.ResponseWriter, r *http.Request, p Provider, profile iam.ExternalProfile, back string) {
	signedIn, err := h.auth.AuthenticatePlatform(r.Context(), p.Platform, profile)
	if errors.Is(err, iam.ErrSignUpRequired) && h.onboarding != nil {
		pending := pendingSignUp{
			Provider: p.Platform, Subject: profile.Subject, Email: profile.Email,
			EmailVerified: profile.EmailVerified, DisplayName: profile.DisplayName, ReturnTo: back,
		}
		sealed, err := h.codec.Encode(signUpCookie, pending, signUpTTL)
		if err != nil {
			h.serverError(w, err)
			return
		}
		h.setCookie(w, signUpCookie, sealed, signUpTTL)
		http.Redirect(w, r, SignUpPath, http.StatusSeeOther)
		return
	}
	if errors.Is(err, iam.ErrSignUpRequired) {
		h.renderError(w, http.StatusForbidden, "There is no account for you here.")
		return
	}
	if err != nil {
		h.externalSignInError(w, p, err)
		return
	}
	if err := h.startSignIn(w, signIn{AccountID: signedIn.AccountID, Method: signedIn.Method}); err != nil {
		h.serverError(w, err)
		return
	}
	h.continueTo(w, r, back)
}

func (h *handlers) pendingSignUp(r *http.Request) (pendingSignUp, bool) {
	var p pendingSignUp
	return p, h.readCookie(r, signUpCookie, &p) && p.Subject != ""
}

func (h *handlers) signUpPage(w http.ResponseWriter, r *http.Request) {
	pending, ok := h.pendingSignUp(r)
	if !ok || h.onboarding == nil {
		h.renderError(w, http.StatusBadRequest, "Start signing up again from the sign-in page.")
		return
	}
	csrf, err := h.csrfToken(w, r)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.renderSignUp(w, http.StatusOK, signUpData{CSRF: csrf, Email: pending.Email})
}

func (h *handlers) renderSignUp(w http.ResponseWriter, status int, d signUpData) {
	d.Title = "Create your organization"
	d.SignUpPath = SignUpPath
	h.render(w, status, signUpContent, "message", d)
}

func (h *handlers) signUp(w http.ResponseWriter, r *http.Request) {
	if !h.checkPost(w, r) {
		return
	}
	pending, ok := h.pendingSignUp(r)
	if !ok || h.onboarding == nil {
		h.renderError(w, http.StatusBadRequest, "This sign-up has expired. Start again from the sign-in page.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	retry := func(status int, message string) {
		csrf, _ := h.csrfToken(w, r)
		h.renderSignUp(w, status, signUpData{Error: message, CSRF: csrf, Email: pending.Email, Name: name, Slug: slug})
	}

	input := iam.OnboardWithPlatformIdentityInput{
		Provider: pending.Provider,
		Profile: iam.ExternalProfile{
			Subject: pending.Subject, Email: pending.Email, EmailVerified: pending.EmailVerified, DisplayName: pending.DisplayName,
		},
		OrganizationName: name,
	}
	if slug != "" {
		input.OrganizationSlug = &slug
	}
	accountID, err := h.onboarding.OnboardWithPlatformIdentity(r.Context(), input)
	if _, conflict := errors.AsType[*onboarding.ErrOrganizationConflict](err); conflict {
		retry(http.StatusConflict, "An organization with this name or address already exists. Choose another.")
		return
	}
	if _, conflict := errors.AsType[*onboarding.ErrAccountConflict](err); conflict {
		h.clearCookie(w, signUpCookie)
		h.renderError(w, http.StatusConflict, "You already have an account. Sign in instead.")
		return
	}
	if invalid, ok := errors.AsType[*iam.ErrInvalidInput](err); ok {
		retry(http.StatusUnprocessableEntity, "Check the form: "+invalid.Message+".")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.clearCookie(w, signUpCookie)
	if err := h.startSignIn(w, signIn{AccountID: accountID, Method: iam.SignInPlatform}); err != nil {
		h.serverError(w, err)
		return
	}
	h.continueTo(w, r, pending.ReturnTo)
}

// canSignUp reports whether signing in with a platform provider can create
// an account.
func (h *handlers) canSignUp() bool {
	if h.onboarding == nil {
		return false
	}
	for _, p := range h.providers {
		if p.Platform != "" {
			return true
		}
	}
	return false
}
