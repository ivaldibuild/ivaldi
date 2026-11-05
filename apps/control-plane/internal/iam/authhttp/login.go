package authhttp

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func (h *handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	csrf, err := h.csrfToken(w, r)
	if err != nil {
		h.serverError(w, err)
		return
	}
	back, _ := returnTo(r.URL.Query().Get("return_to"))
	h.renderLogin(w, http.StatusOK, loginData{CSRF: csrf, ReturnTo: back})
}

func (h *handlers) renderLogin(w http.ResponseWriter, status int, d loginData) {
	d.Title = "Sign in"
	d.LoginPath = LoginPath
	d.Builtin = h.settings.BuiltinAccounts
	d.Providers = h.providerLinks(d.ReturnTo)
	h.render(w, status, loginContent, "login", d)
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	if !h.checkPost(w, r) {
		return
	}
	email := r.PostForm.Get("email")
	back, _ := returnTo(r.PostForm.Get("return_to"))
	retry := func(status int, message string) {
		csrf, _ := h.csrfToken(w, r)
		h.renderLogin(w, status, loginData{Error: message, CSRF: csrf, ReturnTo: back, Email: email})
	}

	signedIn, err := h.auth.AuthenticatePassword(r.Context(), email, r.PostForm.Get("password"))
	switch {
	case errors.Is(err, iam.ErrInvalidCredentials):
		retry(http.StatusUnauthorized, "The email or password is wrong.")
		return
	case errors.Is(err, iam.ErrStatus):
		retry(http.StatusForbidden, "This account cannot sign in. Ask an administrator.")
		return
	case errors.Is(err, iam.ErrNotAdmitted):
		retry(http.StatusForbidden, "Signing in with a password is not enabled.")
		return
	case err != nil:
		h.serverError(w, err)
		return
	}
	if err := h.startSignIn(w, signIn{AccountID: signedIn.AccountID, Method: signedIn.Method}); err != nil {
		h.serverError(w, err)
		return
	}
	h.continueTo(w, r, back)
}

// continueTo sends the browser on to the authorization request it came
// from, or says it signed in when it came from nowhere.
func (h *handlers) continueTo(w http.ResponseWriter, r *http.Request, back string) {
	if back == "" {
		h.renderMessage(w, "Signed in", "You are signed in. You can close this page and return to your application.")
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *handlers) organizationsPage(w http.ResponseWriter, r *http.Request) {
	current, ok := h.currentSignIn(r)
	back, valid := returnTo(r.URL.Query().Get("return_to"))
	if !ok || !valid {
		h.renderError(w, http.StatusBadRequest, "Start signing in from your application again.")
		return
	}
	memberships, err := h.memberships(r, current)
	if err != nil {
		h.serverError(w, err)
		return
	}
	csrf, err := h.csrfToken(w, r)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.render(w, http.StatusOK, organizationsContent, "organizations", organizationsData{
		Title: "Choose an organization", CSRF: csrf, ReturnTo: back,
		OrganizationsPath: OrganizationsPath, Memberships: memberships,
	})
}

// chooseOrganization sends the browser back to the authorization request
// with the chosen Organization, which the endpoint checks again.
func (h *handlers) chooseOrganization(w http.ResponseWriter, r *http.Request) {
	if !h.checkPost(w, r) {
		return
	}
	current, ok := h.currentSignIn(r)
	back, valid := returnTo(r.PostForm.Get("return_to"))
	if !ok || !valid {
		h.renderError(w, http.StatusBadRequest, "Start signing in from your application again.")
		return
	}
	memberships, err := h.memberships(r, current)
	if err != nil {
		h.serverError(w, err)
		return
	}
	chosen, found := choose(memberships, r.PostForm.Get("organization"))
	if !found || r.PostForm.Get("organization") == "" {
		h.renderError(w, http.StatusForbidden, "You are not a member of that organization.")
		return
	}
	http.Redirect(w, r, iam.AppendQuery(back, url.Values{"organization": {chosen.OrganizationID}}), http.StatusSeeOther)
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if !h.checkPost(w, r) {
		return
	}
	h.clearCookie(w, signInCookie)
	h.renderMessage(w, "Signed out", "You are signed out of Ivaldi.")
}
