package variables

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// VariableController serves the Variable operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request.
type VariableController struct {
	service delivery.VariableService
}

var _ api.Routes = (*VariableController)(nil)

func NewVariableController(service delivery.VariableService) *VariableController {
	return &VariableController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *VariableController) RegisterRoutes(h huma.API) {
	register(h, "create-project-variable", http.MethodPost, "/projects/{project_id}/variables", "Create a variable every Application in a Project deploys with", http.StatusCreated, c.CreateProjectVariable)
	register(h, "list-project-variables", http.MethodGet, "/projects/{project_id}/variables", "List a Project's variables", 0, c.ListProjectVariables)
	register(h, "create-application-variable", http.MethodPost, "/applications/{application}/variables", "Create a variable an Application deploys with", http.StatusCreated, c.CreateApplicationVariable)
	register(h, "list-application-variables", http.MethodGet, "/applications/{application}/variables", "List an Application's variables", 0, c.ListApplicationVariables)
	register(h, "get-variable", http.MethodGet, "/variables/{variable_id}", "Get a variable", 0, c.GetVariable)
	register(h, "update-variable", http.MethodPatch, "/variables/{variable_id}", "Change a variable", 0, c.UpdateVariable)
	register(h, "archive-variable", http.MethodPost, "/variables/{variable_id}/archive", "Archive a variable, freeing its name", 0, c.ArchiveVariable)

	register(h, "add-variable-value", http.MethodPost, "/variables/{variable_id}/values", "Add a value to a variable", 0, c.AddVariableValue)
	register(h, "update-variable-value", http.MethodPut, "/variables/{variable_id}/values/{value_id}", "Replace a variable's value", 0, c.UpdateVariableValue)
	register(h, "remove-variable-value", http.MethodDelete, "/variables/{variable_id}/values/{value_id}", "Remove a variable's value", 0, c.RemoveVariableValue)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"variables"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

// caller returns the authenticated caller. The API refuses a secured
// operation without a valid token, so a missing caller is a wiring bug.
func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *VariableController) CreateProjectVariable(ctx context.Context, req *CreateProjectVariableRequest) (*VariableResponse, error) {
	return c.create(ctx, req.Body, delivery.VariableOwner{ProjectID: req.ProjectID})
}

func (c *VariableController) CreateApplicationVariable(ctx context.Context, req *CreateApplicationVariableRequest) (*VariableResponse, error) {
	return c.create(ctx, req.Body, delivery.VariableOwner{ApplicationID: req.ApplicationIdentifier})
}

func (c *VariableController) create(ctx context.Context, body VariableBody, owner delivery.VariableOwner) (*VariableResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	v, err := c.service.CreateVariable(ctx, who.Actor, body.input(who.OrganizationID, owner))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return variableResponse(v), nil
}

func (c *VariableController) ListProjectVariables(ctx context.Context, req *ListProjectVariablesRequest) (*VariableListResponse, error) {
	return c.list(ctx, delivery.ListVariablesQuery{Owner: delivery.VariableOwner{ProjectID: req.ProjectID}})
}

func (c *VariableController) ListApplicationVariables(ctx context.Context, req *ListApplicationVariablesRequest) (*VariableListResponse, error) {
	return c.list(ctx, delivery.ListVariablesQuery{
		Owner:            delivery.VariableOwner{ApplicationID: req.ApplicationIdentifier},
		IncludeInherited: req.IncludeInherited,
	})
}

func (c *VariableController) list(ctx context.Context, query delivery.ListVariablesQuery) (*VariableListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	query.OrganizationID = who.OrganizationID
	vs, err := c.service.ListVariables(ctx, who.Actor, query)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return variableListResponse(vs), nil
}

func (c *VariableController) GetVariable(ctx context.Context, req *VariableRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, c.service.GetVariable)
}

func (c *VariableController) UpdateVariable(ctx context.Context, req *UpdateVariableRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, func(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
		return c.service.UpdateVariable(ctx, actor, organizationID, variableID, req.props())
	})
}

func (c *VariableController) ArchiveVariable(ctx context.Context, req *VariableRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, c.service.ArchiveVariable)
}

func (c *VariableController) AddVariableValue(ctx context.Context, req *AddVariableValueRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, func(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
		return c.service.AddVariableValue(ctx, actor, organizationID, variableID, req.Body.props())
	})
}

func (c *VariableController) UpdateVariableValue(ctx context.Context, req *UpdateVariableValueRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, func(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
		return c.service.UpdateVariableValue(ctx, actor, organizationID, variableID, req.ValueID, req.Body.props())
	})
}

func (c *VariableController) RemoveVariableValue(ctx context.Context, req *VariableValueRequest) (*VariableResponse, error) {
	return c.variable(ctx, req.VariableID, func(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
		return c.service.RemoveVariableValue(ctx, actor, organizationID, variableID, req.ValueID)
	})
}

// variable calls an operation on one Variable.
func (c *VariableController) variable(ctx context.Context, variableID string, op func(context.Context, identity.Actor, string, string) (*delivery.Variable, error)) (*VariableResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	v, err := op(ctx, who.Actor, who.OrganizationID, variableID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return variableResponse(v), nil
}
