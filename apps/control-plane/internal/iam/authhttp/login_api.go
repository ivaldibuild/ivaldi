package authhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	ivaldiapi "github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// The frontend's sign-in screens call these operations, under /v1. Every
// step carries the login challenge from the frontend's URL. A step answers
// with redirect_to, where the frontend sends the browser to finish, or with
// organizations to choose from and a login_session to choose with.

type loginRequestInput struct {
	LoginChallenge string `query:"login_challenge" required:"true" doc:"The login_challenge the authorization server sent the browser with"`
}

type providerOption struct {
	Kind     string `json:"kind" doc:"The provider, such as github"`
	Name     string `json:"name" doc:"The provider's name, for its button"`
	StartURL string `json:"start_url" doc:"Where to send the browser to sign in with the provider"`
}

type loginRequestOutput struct {
	Body struct {
		ClientID   string           `json:"client_id"`
		ClientName string           `json:"client_name" doc:"The application asking the person to sign in"`
		Password   bool             `json:"password" doc:"Whether email and password sign-in is offered"`
		SignUp     bool             `json:"sign_up" doc:"Whether signing in with a provider can create an account"`
		Providers  []providerOption `json:"providers"`
	}
}

type passwordInput struct {
	Body struct {
		LoginChallenge string `json:"login_challenge" required:"true"`
		Email          string `json:"email" required:"true" format:"email"`
		Password       string `json:"password" required:"true"`
	}
}

type organizationsInput struct {
	LoginChallenge string `query:"login_challenge" required:"true"`
	LoginSession   string `query:"login_session" required:"true" doc:"The login_session from the step that asked for a choice"`
}

type organizationInput struct {
	Body struct {
		LoginChallenge string `json:"login_challenge" required:"true"`
		LoginSession   string `json:"login_session" required:"true" doc:"The login_session from the step that asked for a choice"`
		OrganizationID string `json:"organization_id" required:"true"`
	}
}

type signUpInput struct {
	Body struct {
		LoginChallenge   string  `json:"login_challenge" required:"true"`
		SignUpToken      string  `json:"signup_token" required:"true" doc:"The signup_token the provider sign-in sent the browser back with"`
		OrganizationName string  `json:"organization_name" required:"true" minLength:"1"`
		OrganizationSlug *string `json:"organization_slug,omitempty" required:"false" doc:"Derived from the name when absent"`
	}
}

type organizationOption struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type loginStepOutput struct {
	Body struct {
		RedirectTo    string               `json:"redirect_to,omitempty" doc:"Send the browser here to finish signing in"`
		LoginSession  string               `json:"login_session,omitempty" doc:"Pass back with the chosen organization"`
		Organizations []organizationOption `json:"organizations,omitempty" doc:"The organizations the person may choose from"`
	}
}

func (h *handlers) RegisterRoutes(api huma.API) {
	tags := []string{"sign-in"}
	// Signing in is how a caller gets a token, so these need none.
	public := map[string]any{ivaldiapi.Public: true}
	huma.Register(api, huma.Operation{
		OperationID: "get-login-request", Metadata: public, Method: http.MethodGet, Path: "/auth/login", Tags: tags,
		Summary: "Describe a sign-in: the asking application and the ways to sign in",
	}, h.loginRequest)
	huma.Register(api, huma.Operation{
		OperationID: "login-with-password", Metadata: public, Method: http.MethodPost, Path: "/auth/login/password", Tags: tags,
		Summary: "Sign in with an email and password",
	}, h.loginWithPassword)
	huma.Register(api, huma.Operation{
		OperationID: "list-login-organizations", Metadata: public, Method: http.MethodGet, Path: "/auth/login/organizations", Tags: tags,
		Summary: "List the organizations a login session may choose from",
	}, h.listOrganizations)
	huma.Register(api, huma.Operation{
		OperationID: "choose-login-organization", Metadata: public, Method: http.MethodPost, Path: "/auth/login/organization", Tags: tags,
		Summary: "Choose the organization to sign in to",
	}, h.chooseOrganization)
	huma.Register(api, huma.Operation{
		OperationID: "sign-up", Metadata: public, Method: http.MethodPost, Path: "/auth/signup", Tags: tags,
		Summary: "Sign up with a provider identity and create an organization",
	}, h.signUp)
}

func (h *handlers) loginRequest(_ context.Context, in *loginRequestInput) (*loginRequestOutput, error) {
	ch, err := h.openChallenge(in.LoginChallenge)
	if err != nil {
		return nil, h.apiError(err)
	}
	out := &loginRequestOutput{}
	out.Body.ClientID = ch.Auth.ClientID
	out.Body.ClientName = ch.Auth.ClientName
	out.Body.Password = h.settings.BuiltinAccounts
	out.Body.Providers = []providerOption{}
	for _, p := range h.providers {
		out.Body.Providers = append(out.Body.Providers, providerOption{Kind: p.Kind, Name: p.Connector.DisplayName(), StartURL: h.startURL(p.Kind, in.LoginChallenge)})
		if p.Platform != "" && h.onboarding != nil {
			out.Body.SignUp = true
		}
	}
	return out, nil
}

func (h *handlers) loginWithPassword(ctx context.Context, in *passwordInput) (*loginStepOutput, error) {
	ch, err := h.openChallenge(in.Body.LoginChallenge)
	if err != nil {
		return nil, h.apiError(err)
	}
	signedIn, err := h.auth.AuthenticatePassword(ctx, in.Body.Email, in.Body.Password)
	if err != nil {
		return nil, h.apiError(err)
	}
	return h.step(ctx, ch, signInOf(signedIn), "")
}

// listOrganizations answers the organizations a login session may choose
// from, for a browser the provider sent back with only the challenge and
// the session. It opens them the way the organization step does, so a
// mismatched or expired pair gets the same 400.
func (h *handlers) listOrganizations(ctx context.Context, in *organizationsInput) (*loginStepOutput, error) {
	ch, err := h.openChallenge(in.LoginChallenge)
	if err != nil {
		return nil, h.apiError(err)
	}
	var s loginSession
	if err := h.open(sessionName, in.LoginSession, &s, func() string { return s.Nonce }, ch); err != nil {
		return nil, h.apiError(err)
	}
	memberships, err := h.memberships(ctx, s.SignIn)
	if err != nil {
		return nil, h.apiError(err)
	}
	out := &loginStepOutput{}
	for _, m := range memberships {
		out.Body.Organizations = append(out.Body.Organizations, organizationOption{ID: m.OrganizationID, Slug: m.OrganizationSlug, Name: m.OrganizationName})
	}
	return out, nil
}

func (h *handlers) chooseOrganization(ctx context.Context, in *organizationInput) (*loginStepOutput, error) {
	ch, err := h.openChallenge(in.Body.LoginChallenge)
	if err != nil {
		return nil, h.apiError(err)
	}
	var s loginSession
	if err := h.open(sessionName, in.Body.LoginSession, &s, func() string { return s.Nonce }, ch); err != nil {
		return nil, h.apiError(err)
	}
	out, err := h.step(ctx, ch, s.SignIn, in.Body.OrganizationID)
	if err == nil && out.Body.RedirectTo == "" {
		return nil, huma.Error403Forbidden("you are not a member of that organization")
	}
	return out, err
}

func (h *handlers) signUp(ctx context.Context, in *signUpInput) (*loginStepOutput, error) {
	if h.onboarding == nil {
		return nil, huma.Error403Forbidden("signing up is not available")
	}
	ch, err := h.openChallenge(in.Body.LoginChallenge)
	if err != nil {
		return nil, h.apiError(err)
	}
	var t signUpToken
	if err := h.open(signUpName, in.Body.SignUpToken, &t, func() string { return t.Nonce }, ch); err != nil {
		return nil, h.apiError(err)
	}
	input := iam.OnboardWithPlatformIdentityInput{
		Provider: t.Provider,
		Profile: iam.ExternalProfile{
			Subject: t.Subject, Email: t.Email, EmailVerified: t.EmailVerified, DisplayName: t.DisplayName,
		},
		OrganizationName: strings.TrimSpace(in.Body.OrganizationName),
		OrganizationSlug: in.Body.OrganizationSlug,
	}
	accountID, err := h.onboarding.OnboardWithPlatformIdentity(ctx, input)
	if err != nil {
		return nil, h.apiError(err)
	}
	return h.step(ctx, ch, signIn{AccountID: accountID, Method: iam.SignInPlatform}, "")
}

func (h *handlers) step(ctx context.Context, ch loginChallenge, s signIn, organization string) (*loginStepOutput, error) {
	step, err := h.next(ctx, ch, s, organization)
	if err != nil {
		return nil, h.apiError(err)
	}
	out := &loginStepOutput{}
	out.Body.RedirectTo = step.RedirectTo
	out.Body.LoginSession = step.LoginSession
	for _, m := range step.Organizations {
		out.Body.Organizations = append(out.Body.Organizations, organizationOption{ID: m.OrganizationID, Slug: m.OrganizationSlug, Name: m.OrganizationName})
	}
	return out, nil
}

// apiError turns a sign-in refusal into a problem response the frontend can
// show. Anything unexpected is logged, and the caller learns only that it
// failed.
func (h *handlers) apiError(err error) error {
	if _, ok := errors.AsType[*onboarding.ErrOrganizationConflict](err); ok {
		return huma.Error409Conflict("an organization with this name or address already exists")
	}
	if _, ok := errors.AsType[*onboarding.ErrAccountConflict](err); ok {
		return huma.Error409Conflict("you already have an account; sign in instead")
	}
	if invalid, ok := errors.AsType[*iam.ErrInvalidInput](err); ok {
		return huma.Error422UnprocessableEntity(invalid.Message)
	}
	switch {
	case errors.Is(err, errExpired):
		return huma.Error400BadRequest(errExpired.Error())
	case errors.Is(err, iam.ErrInvalidCredentials):
		return huma.Error401Unauthorized("the email or password is wrong")
	case errors.Is(err, iam.ErrStatus):
		return huma.Error403Forbidden("this account cannot sign in; ask an administrator")
	case errors.Is(err, iam.ErrNotAdmitted):
		return huma.Error403Forbidden("this way of signing in is not allowed here")
	case errors.Is(err, iam.ErrNoMembership):
		return huma.Error403Forbidden("you are not a member of any organization you can use")
	}
	h.logger.Error("sign-in step failed", logging.F("error", err))
	return huma.Error500InternalServerError("signing in failed")
}
