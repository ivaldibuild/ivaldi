package serviceaccounts

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves service accounts and their API tokens for the caller's
// Organization, which comes from the token, never from the request.
type Controller struct {
	service iam.ServiceAccountService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service iam.ServiceAccountService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-service-account", http.MethodPost, "/service-accounts", "Create a service account for CI or another integration", http.StatusCreated, c.Create)
	register(h, "list-service-accounts", http.MethodGet, "/service-accounts", "List the Organization's service accounts", 0, c.List)
	register(h, "get-service-account", http.MethodGet, "/service-accounts/{service_account_id}", "Get a service account", 0, c.Get)
	register(h, "suspend-service-account", http.MethodPost, "/service-accounts/{service_account_id}/suspend", "Suspend a service account, so none of its tokens work", 0, c.Suspend)
	register(h, "reactivate-service-account", http.MethodPost, "/service-accounts/{service_account_id}/reactivate", "Reactivate a suspended service account", 0, c.Reactivate)
	register(h, "create-api-token", http.MethodPost, "/service-accounts/{service_account_id}/tokens", "Issue an API token for a service account; its secret is shown once", http.StatusCreated, c.CreateToken)
	register(h, "list-api-tokens", http.MethodGet, "/service-accounts/{service_account_id}/tokens", "List a service account's API tokens, without their secrets", 0, c.ListTokens)
	register(h, "revoke-api-token", http.MethodPost, "/api-tokens/{api_token_id}/revoke", "Revoke an API token for good", 0, c.RevokeToken)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"service accounts"}, DefaultStatus: status, Security: bearer,
	}, handler)
}

type CreateRequest struct {
	Body struct {
		DisplayName string `json:"display_name" required:"true" minLength:"1" maxLength:"255" doc:"Its name, such as GitHub Actions"`
		Description string `json:"description,omitempty" required:"false" doc:"What it is for"`
	}
}

type ListRequest struct {
	IncludeInactive bool `query:"include_inactive" doc:"Also list suspended service accounts"`
}

type AccountRequest struct {
	ServiceAccountID string `path:"service_account_id" doc:"The service account"`
}

type CreateTokenRequest struct {
	ServiceAccountID string `path:"service_account_id" doc:"The service account"`
	Body             struct {
		Name      string     `json:"name" required:"true" minLength:"1" maxLength:"255" doc:"What the token is for, such as github-actions"`
		ExpiresAt *time.Time `json:"expires_at,omitempty" required:"false" doc:"When it stops working; it never does when omitted"`
	}
}

type TokenRequest struct {
	APITokenID string `path:"api_token_id" doc:"The API token"`
}

// ServiceAccount is a service account as the API shows it.
type ServiceAccount struct {
	ID                   string              `json:"id"`
	DisplayName          string              `json:"display_name"`
	Description          string              `json:"description"`
	Status               iam.PrincipalStatus `json:"status"`
	CreatedByPrincipalID string              `json:"created_by_principal_id"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

func view(sa *iam.ServiceAccount) ServiceAccount {
	s := sa.Snapshot()
	return ServiceAccount{
		ID: s.ID, DisplayName: s.DisplayName, Description: s.Description, Status: s.Status,
		CreatedByPrincipalID: s.CreatedByPrincipalID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// IssuedToken is a new token with its secret, which is never shown again.
type IssuedToken struct {
	Token  iam.APITokenSnapshot `json:"token"`
	Secret string               `json:"secret" doc:"The token itself. Store it now: Ivaldi keeps only its hash."`
}

type AccountResponse struct {
	Body api.Response[ServiceAccount]
}

type AccountListResponse struct {
	Body api.Response[[]ServiceAccount]
}

type IssuedTokenResponse struct {
	Body api.Response[IssuedToken]
}

type TokenResponse struct {
	Body api.Response[iam.APITokenSnapshot]
}

type TokenListResponse struct {
	Body api.Response[[]iam.APITokenSnapshot]
}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) Create(ctx context.Context, req *CreateRequest) (*AccountResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	sa, err := c.service.CreateServiceAccount(ctx, who.Actor, iam.CreateServiceAccountInput{
		OrganizationID: who.OrganizationID, DisplayName: req.Body.DisplayName, Description: req.Body.Description,
	})
	return accountResponse(ctx, sa, err)
}

func (c *Controller) List(ctx context.Context, req *ListRequest) (*AccountListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	all, err := c.service.ListServiceAccounts(ctx, who.Actor, who.OrganizationID, req.IncludeInactive)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	items := make([]ServiceAccount, len(all))
	for i, sa := range all {
		items[i] = view(sa)
	}
	return &AccountListResponse{Body: api.SuccessResponse(nil, items)}, nil
}

func (c *Controller) Get(ctx context.Context, req *AccountRequest) (*AccountResponse, error) {
	return c.onAccount(ctx, req.ServiceAccountID, c.service.GetServiceAccount)
}

func (c *Controller) Suspend(ctx context.Context, req *AccountRequest) (*AccountResponse, error) {
	return c.onAccount(ctx, req.ServiceAccountID, c.service.SuspendServiceAccount)
}

func (c *Controller) Reactivate(ctx context.Context, req *AccountRequest) (*AccountResponse, error) {
	return c.onAccount(ctx, req.ServiceAccountID, c.service.ReactivateServiceAccount)
}

func (c *Controller) onAccount(ctx context.Context, id string, op func(context.Context, identity.Actor, string, string) (*iam.ServiceAccount, error)) (*AccountResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	sa, err := op(ctx, who.Actor, who.OrganizationID, id)
	return accountResponse(ctx, sa, err)
}

func accountResponse(ctx context.Context, sa *iam.ServiceAccount, err error) (*AccountResponse, error) {
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &AccountResponse{Body: api.SuccessResponse(nil, view(sa))}, nil
}

func (c *Controller) CreateToken(ctx context.Context, req *CreateTokenRequest) (*IssuedTokenResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	token, secret, err := c.service.CreateAPIToken(ctx, who.Actor, iam.CreateAPITokenInput{
		OrganizationID: who.OrganizationID, ServiceAccountID: req.ServiceAccountID,
		Name: req.Body.Name, ExpiresAt: req.Body.ExpiresAt,
	})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &IssuedTokenResponse{Body: api.SuccessResponse(nil, IssuedToken{Token: token.Snapshot(), Secret: secret})}, nil
}

func (c *Controller) ListTokens(ctx context.Context, req *AccountRequest) (*TokenListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := c.service.ListAPITokens(ctx, who.Actor, who.OrganizationID, req.ServiceAccountID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	items := make([]iam.APITokenSnapshot, len(tokens))
	for i, t := range tokens {
		items[i] = t.Snapshot()
	}
	return &TokenListResponse{Body: api.SuccessResponse(nil, items)}, nil
}

func (c *Controller) RevokeToken(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	token, err := c.service.RevokeAPIToken(ctx, who.Actor, who.OrganizationID, req.APITokenID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &TokenResponse{Body: api.SuccessResponse(nil, token.Snapshot())}, nil
}

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail.
func httpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, iam.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, iam.ErrStatus), errors.Is(err, iam.ErrAlreadyExists):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
