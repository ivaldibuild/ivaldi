package principals

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves GET /principals for the caller's Organization, which
// comes from the token, never from the request.
type Controller struct {
	service iam.PrincipalService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service iam.PrincipalService) *Controller {
	return &Controller{service: service}
}

func (c *Controller) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID: "list-principals",
		Method:      http.MethodGet,
		Path:        "/principals",
		Summary:     "Name principals by ID",
		Description: "Names the requested principals of the caller's Organization, whatever their kind: users, " +
			"service accounts, Agents, agent Sessions, and workloads. An Agent also names its Teams, archived ones included. " +
			"IDs that name no principal of the Organization are left out rather than refused, so a page does not " +
			"fail because one principal was deleted.",
		Tags:     []string{"principals"},
		Security: []map[string][]string{{api.BearerAuth: {}}},
	}, c.ListPrincipals)
}

type ListPrincipalsRequest struct {
	IDs []string `query:"ids" required:"true" minItems:"1" doc:"The principal IDs to name, comma-separated, such as prn_a,prn_b; at most 100 distinct IDs"`
}

type ListPrincipalsResponse struct {
	Body api.Response[[]NamedPrincipal]
}

// NamedPrincipal is one principal as the API shows it: enough to name who
// created or acted on something, with no emails or credentials.
type NamedPrincipal struct {
	ID          string              `json:"id"`
	Kind        iam.PrincipalKind   `json:"kind" enum:"user,service_account,agent,agent_session,workload"`
	DisplayName string              `json:"display_name"`
	Status      iam.PrincipalStatus `json:"status" enum:"active,suspended,deactivated"`
	// Teams are the Teams an Agent belongs to, archived ones included with
	// their archived_at; every other kind has none. An Agent is accountable to its Teams (ADR 0007).
	Teams []iam.PrincipalTeam `json:"teams" doc:"The Teams an Agent belongs to, archived ones included with their archived_at; empty for every other kind"`
}

func (c *Controller) ListPrincipals(ctx context.Context, req *ListPrincipalsRequest) (*ListPrincipalsResponse, error) {
	who, ok := identity.CallerFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("an access token is required")
	}
	found, err := c.service.ListPrincipals(ctx, who.Actor, iam.ListPrincipalsQuery{
		OrganizationID: who.OrganizationID, IDs: req.IDs,
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	items := make([]NamedPrincipal, len(found))
	for i, p := range found {
		items[i] = NamedPrincipal{
			ID: p.Principal.ID, Kind: p.Principal.Kind, DisplayName: p.Principal.DisplayName,
			Status: p.Principal.Status, Teams: p.Teams,
		}
	}
	return &ListPrincipalsResponse{Body: api.SuccessResponse(nil, items)}, nil
}

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
