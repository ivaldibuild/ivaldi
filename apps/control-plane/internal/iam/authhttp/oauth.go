package authhttp

import (
	"crypto/subtle"
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

// authorize is the authorization endpoint. A browser already signed in to
// the authorization server gets its code straight away when the
// Organization is clear; otherwise the frontend signs the person in, with
// a login challenge carrying the request.
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
		var oe *iam.OAuthError
		switch {
		case errors.As(err, &oe) && oe.RedirectURI != "":
			redirect(w, oe.Location(h.settings.Issuer))
		case errors.As(err, &oe):
			// The client or its redirect URI cannot be trusted, so the
			// error goes to the frontend, never to the client.
			h.toFrontend(w, r, url.Values{"error": {oe.Code}, "error_description": {oe.Description}})
		default:
			h.serverError(w, r, "", err)
		}
		return
	}
	toClient := func(code, description string) {
		e := &iam.OAuthError{Code: code, Description: description, RedirectURI: v.RedirectURI, State: v.State}
		redirect(w, e.Location(h.settings.Issuer))
	}

	prompt := q.Get("prompt")
	hint := q.Get("organization")
	current, signedIn := h.currentSignIn(r)
	if signedIn && prompt != "login" {
		memberships, err := h.memberships(ctx, current)
		if err != nil {
			h.serverError(w, r, "", err)
			return
		}
		if chosen, ok := choose(memberships, hint); ok {
			h.issue(w, r, v, current, chosen.OrganizationID)
			return
		}
		if len(memberships) == 0 {
			toClient(iam.OAuthAccessDenied, "the account belongs to no organization it can use")
			return
		}
		if prompt == "none" {
			toClient("interaction_required", "an organization must be chosen")
			return
		}
		// Signed in, but the person must choose: the frontend asks.
		challenge, ch, err := h.newChallenge(w, v, hint)
		if err != nil {
			h.serverError(w, r, "", err)
			return
		}
		session, err := h.codec.Encode(sessionName, loginSession{Nonce: ch.Nonce, SignIn: current}, loginFlowTTL)
		if err != nil {
			h.serverError(w, r, "", err)
			return
		}
		h.toFrontend(w, r, url.Values{"login_challenge": {challenge}, "login_session": {session}})
		return
	}
	if prompt == "none" {
		toClient(iam.OAuthLoginRequired, "the person is not signed in")
		return
	}
	challenge, _, err := h.newChallenge(w, v, hint)
	if err != nil {
		h.serverError(w, r, "", err)
		return
	}
	h.toFrontend(w, r, url.Values{"login_challenge": {challenge}})
}

// continueAuthorization redeems a login verifier: only the browser that
// started the challenge can, because it holds the challenge's nonce in its
// login cookie. It remembers the sign-in and sends the code to the client.
func (h *handlers) continueAuthorization(w http.ResponseWriter, r *http.Request) {
	var v loginVerifier
	var nonce string
	if h.codec.Decode(verifierName, r.URL.Query().Get("login_verifier"), &v) != nil ||
		!h.readCookie(r, loginCookie, &nonce) ||
		subtle.ConstantTimeCompare([]byte(nonce), []byte(v.Nonce)) != 1 {
		h.failToFrontend(w, r, "", errorExpired)
		return
	}
	h.clearCookie(w, loginCookie)
	h.issue(w, r, &v.Auth, v.SignIn, v.OrganizationID)
}

// issue checks the membership once more, remembers the sign-in, and sends
// the browser back to the client with a code.
func (h *handlers) issue(w http.ResponseWriter, r *http.Request, v *iam.ValidatedAuthorization, s signIn, organizationID string) {
	m, err := h.auth.Membership(r.Context(), s.AccountID, organizationID)
	if errors.Is(err, iam.ErrNoMembership) || (err == nil && !s.allows(m.OrganizationID)) {
		e := &iam.OAuthError{Code: iam.OAuthAccessDenied, Description: "the membership is not active", RedirectURI: v.RedirectURI, State: v.State}
		redirect(w, e.Location(h.settings.Issuer))
		return
	}
	if err != nil {
		h.serverError(w, r, "", err)
		return
	}
	if err := h.sealCookie(w, signInCookie, s, h.settings.SessionTTL); err != nil {
		h.serverError(w, r, "", err)
		return
	}
	location, err := h.oauth.IssueCode(r.Context(), v, *m)
	if err != nil {
		h.serverError(w, r, "", err)
		return
	}
	redirect(w, location)
}

// logout ends the browser's sign-in at the authorization server and returns
// to the frontend. Clients revoke their own refresh tokens.
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	h.clearCookie(w, signInCookie)
	h.toFrontend(w, r, url.Values{"logged_out": {"true"}})
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
