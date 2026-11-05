package authhttp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// ContinuePath finishes an authorization request once the person signed in:
// the frontend sends the browser there with a login verifier.
const ContinuePath = "/oauth/authorize/continue"

// LogoutPath ends the browser's sign-in at the authorization server.
const LogoutPath = "/auth/logout"

// Names sealed values are bound to, so one cannot be passed off as another.
const (
	challengeName = "login-challenge"
	sessionName   = "login-session"
	verifierName  = "login-verifier"
	signUpName    = "signup"
)

// Error codes the frontend gets in its error query parameter.
const (
	errorExpired       = "login_expired"
	errorCancelled     = "cancelled"
	errorProvider      = "provider_failed"
	errorNotAdmitted   = "not_admitted"
	errorNoMembership  = "no_membership"
	errorAccountExists = "account_exists"
	errorDisabled      = "account_disabled"
	errorServer        = "server_error"
)

// loginFlowTTL bounds a sign-in, from the authorization request to its
// last step.
const loginFlowTTL = 15 * time.Minute

// errExpired means a challenge, session, verifier, or sign-up token was
// tampered with, has expired, or belongs to another sign-in.
var errExpired = errors.New("the sign-in expired; start again")

// loginChallenge is an authorization request waiting for the person to sign
// in. The frontend holds it sealed and passes it back at every step.
type loginChallenge struct {
	Auth  iam.ValidatedAuthorization `json:"a"`
	Nonce string                     `json:"n"`
	// Hint is the organization the client asked for, by ID or slug.
	Hint string `json:"h,omitempty"`
}

// loginSession is a sign-in that still has to choose an Organization.
type loginSession struct {
	Nonce  string `json:"n"`
	SignIn signIn `json:"s"`
}

// loginVerifier is a finished sign-in, which the browser that started the
// challenge redeems at ContinuePath.
type loginVerifier struct {
	Nonce          string                     `json:"n"`
	SignIn         signIn                     `json:"s"`
	OrganizationID string                     `json:"o"`
	Auth           iam.ValidatedAuthorization `json:"a"`
}

// signUpToken is a platform provider's word for a person with no Account,
// kept until they name their Organization.
type signUpToken struct {
	Nonce         string               `json:"n"`
	Provider      iam.PlatformProvider `json:"p"`
	Subject       string               `json:"s"`
	Email         string               `json:"e"`
	EmailVerified bool                 `json:"ev"`
	DisplayName   string               `json:"d,omitempty"`
}

// loginStep is what follows a sign-in step: either a redirect to finish
// the authorization, or a choice of Organization.
type loginStep struct {
	RedirectTo    string
	LoginSession  string
	Organizations []iam.Membership
}

// newChallenge seals an authorization request and binds it to the browser
// with the login cookie.
func (h *handlers) newChallenge(w http.ResponseWriter, v *iam.ValidatedAuthorization, hint string) (string, loginChallenge, error) {
	ch := loginChallenge{Auth: *v, Nonce: rand.Text(), Hint: hint}
	sealed, err := h.codec.Encode(challengeName, ch, loginFlowTTL)
	if err != nil {
		return "", ch, err
	}
	if err := h.sealCookie(w, loginCookie, ch.Nonce, loginFlowTTL); err != nil {
		return "", ch, err
	}
	return sealed, ch, nil
}

func (h *handlers) openChallenge(raw string) (loginChallenge, error) {
	var ch loginChallenge
	if raw == "" || h.codec.Decode(challengeName, raw, &ch) != nil {
		return ch, errExpired
	}
	return ch, nil
}

// open unseals a value issued for the challenge.
func (h *handlers) open(name, raw string, v any, nonce func() string, ch loginChallenge) error {
	if raw == "" || h.codec.Decode(name, raw, v) != nil {
		return errExpired
	}
	if subtle.ConstantTimeCompare([]byte(nonce()), []byte(ch.Nonce)) != 1 {
		return errExpired
	}
	return nil
}

// next decides what follows a sign-in: finish the authorization in the one
// Organization it can act in, or the one the client or person chose; or ask
// the person to choose.
func (h *handlers) next(ctx context.Context, ch loginChallenge, s signIn, organization string) (loginStep, error) {
	memberships, err := h.memberships(ctx, s)
	if err != nil {
		return loginStep{}, err
	}
	if len(memberships) == 0 {
		return loginStep{}, iam.ErrNoMembership
	}
	if organization == "" {
		organization = ch.Hint
	}
	chosen, ok := choose(memberships, organization)
	if !ok {
		if organization != "" && len(memberships) == 1 {
			return loginStep{}, iam.ErrNoMembership
		}
		session, err := h.codec.Encode(sessionName, loginSession{Nonce: ch.Nonce, SignIn: s}, loginFlowTTL)
		if err != nil {
			return loginStep{}, err
		}
		return loginStep{LoginSession: session, Organizations: memberships}, nil
	}
	verifier, err := h.codec.Encode(verifierName, loginVerifier{Nonce: ch.Nonce, SignIn: s, OrganizationID: chosen.OrganizationID, Auth: ch.Auth}, loginFlowTTL)
	if err != nil {
		return loginStep{}, err
	}
	return loginStep{RedirectTo: h.settings.Issuer + ContinuePath + "?" + url.Values{"login_verifier": {verifier}}.Encode()}, nil
}

// memberships returns the memberships the sign-in may use.
func (h *handlers) memberships(ctx context.Context, s signIn) ([]iam.Membership, error) {
	all, err := h.auth.Memberships(ctx, s.AccountID)
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

// choose picks the membership a hint names, by ID or slug, or the only one
// when there is no hint.
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

// errorCode names a sign-in refusal for the frontend.
func errorCode(err error) string {
	switch {
	case errors.Is(err, errExpired):
		return errorExpired
	case errors.Is(err, iam.ErrNotAdmitted):
		return errorNotAdmitted
	case errors.Is(err, iam.ErrNoMembership):
		return errorNoMembership
	case errors.Is(err, iam.ErrAccountExists):
		return errorAccountExists
	case errors.Is(err, iam.ErrStatus):
		return errorDisabled
	}
	return errorServer
}
