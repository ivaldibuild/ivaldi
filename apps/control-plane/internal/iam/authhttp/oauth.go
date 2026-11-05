package authhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

func (h *handlers) metadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "max-age=3600")
	writeJSON(w, http.StatusOK, h.oauth.Metadata())
}

// authorize is the authorization endpoint. It sends the person to sign in
// when they have not, lets them choose an Organization when they belong to
// several, and then sends a code back to the client.
func (h *handlers) authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	v, err := h.oauth.ValidateAuthorization(ctx, iam.AuthorizeRequest{
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		Resource:            q.Get("resource"),
		Scope:               q.Get("scope"),
	})
	if err != nil {
		h.authorizeError(w, r, err)
		return
	}
	fail := func(code, description string) {
		e := &iam.OAuthError{Code: code, Description: description, RedirectURI: v.RedirectURI, State: v.State}
		http.Redirect(w, r, e.Location(h.settings.Issuer), http.StatusFound)
	}

	prompt := q.Get("prompt")
	current, signedIn := h.currentSignIn(r)
	if prompt == "login" {
		signedIn = false
	}
	if !signedIn {
		if prompt == "none" {
			fail(iam.OAuthLoginRequired, "the person is not signed in")
			return
		}
		// Coming back from the login page must not ask for a login again.
		q.Del("prompt")
		back := iam.OAuthAuthorizePath + "?" + q.Encode()
		http.Redirect(w, r, LoginPath+"?"+url.Values{"return_to": {back}}.Encode(), http.StatusFound)
		return
	}

	memberships, err := h.memberships(r, current)
	if err != nil {
		h.serverError(w, err)
		return
	}
	chosen, ok := choose(memberships, q.Get("organization"))
	switch {
	case len(memberships) == 0:
		fail(iam.OAuthAccessDenied, "the account belongs to no organization it can use")
		return
	case !ok:
		if prompt == "none" {
			fail("interaction_required", "an organization must be chosen")
			return
		}
		back := iam.OAuthAuthorizePath + "?" + q.Encode()
		http.Redirect(w, r, OrganizationsPath+"?"+url.Values{"return_to": {back}}.Encode(), http.StatusFound)
		return
	}

	// The membership is checked again now: it may have ended since sign-in.
	m, err := h.auth.Membership(ctx, current.AccountID, chosen.OrganizationID)
	if errors.Is(err, iam.ErrNoMembership) {
		fail(iam.OAuthAccessDenied, "the membership is no longer active")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}
	location, err := h.oauth.IssueCode(ctx, v, *m)
	if err != nil {
		h.serverError(w, err)
		return
	}
	http.Redirect(w, r, location, http.StatusFound)
}

// memberships returns the memberships the sign-in may use.
func (h *handlers) memberships(r *http.Request, s signIn) ([]iam.Membership, error) {
	all, err := h.auth.Memberships(r.Context(), s.AccountID)
	if err != nil {
		return nil, err
	}
	var usable []iam.Membership
	for _, m := range all {
		if s.allows(m.OrganizationID) {
			usable = append(usable, m)
		}
	}
	return usable, nil
}

// choose picks the membership a hint names, by ID or slug, or the only one.
func choose(memberships []iam.Membership, hint string) (iam.Membership, bool) {
	if hint != "" {
		for _, m := range memberships {
			if m.OrganizationID == hint || m.OrganizationSlug == hint {
				return m, true
			}
		}
		return iam.Membership{}, false
	}
	if len(memberships) == 1 {
		return memberships[0], true
	}
	return iam.Membership{}, false
}

func (h *handlers) authorizeError(w http.ResponseWriter, r *http.Request, err error) {
	var oe *iam.OAuthError
	if !errors.As(err, &oe) {
		h.serverError(w, err)
		return
	}
	if oe.RedirectURI != "" {
		http.Redirect(w, r, oe.Location(h.settings.Issuer), http.StatusFound)
		return
	}
	h.renderError(w, http.StatusBadRequest, "The application that sent you here made an invalid request: "+oe.Description+".")
}

// token is the token endpoint (RFC 6749 section 3.2).
func (h *handlers) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "the request body must be a form"})
		return
	}
	f := r.PostForm
	resp, err := h.oauth.Exchange(r.Context(), iam.TokenRequest{
		GrantType:    f.Get("grant_type"),
		ClientID:     f.Get("client_id"),
		Code:         f.Get("code"),
		RedirectURI:  f.Get("redirect_uri"),
		CodeVerifier: f.Get("code_verifier"),
		RefreshToken: f.Get("refresh_token"),
		Resource:     f.Get("resource"),
		Scope:        f.Get("scope"),
	})
	if err != nil {
		var oe *iam.OAuthError
		if !errors.As(err, &oe) {
			h.logger.Error("token endpoint", logging.F("error", err))
			oe = &iam.OAuthError{Code: iam.OAuthServerError}
		}
		writeOAuthError(w, oe)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

// revoke is the revocation endpoint (RFC 7009).
func (h *handlers) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "the request body must be a form"})
		return
	}
	if err := h.oauth.Revoke(r.Context(), r.PostForm.Get("token"), r.PostForm.Get("client_id")); err != nil {
		h.logger.Error("revocation endpoint", logging.F("error", err))
		writeOAuthError(w, &iam.OAuthError{Code: iam.OAuthServerError})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func writeOAuthError(w http.ResponseWriter, e *iam.OAuthError) {
	status := http.StatusBadRequest
	switch e.Code {
	case iam.OAuthInvalidClient:
		status = http.StatusUnauthorized
	case iam.OAuthServerError:
		status = http.StatusInternalServerError
	}
	body := map[string]string{"error": e.Code}
	if e.Description != "" {
		body["error_description"] = e.Description
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handlers) serverError(w http.ResponseWriter, err error) {
	h.logger.Error("sign-in failed", logging.F("error", err))
	h.renderError(w, http.StatusInternalServerError, "Something went wrong on our side. Try again in a moment.")
}
