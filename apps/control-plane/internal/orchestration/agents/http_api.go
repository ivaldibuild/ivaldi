package agents

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// Controller serves the Agent operations for the caller's Organization,
// which comes from the token, never from the request.
type Controller struct {
	service orchestration.AgentService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service orchestration.AgentService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-agent", http.MethodPost, "/agents", "Create an Agent as a member of a Team", http.StatusCreated, c.CreateAgent)
	register(h, "list-agents", http.MethodGet, "/agents", "List the Organization's Agents", 0, c.ListAgents)
	register(h, "get-agent", http.MethodGet, "/agents/{agent_id}", "Get an Agent with its Team memberships and system prompt", 0, c.GetAgent)
	register(h, "update-agent", http.MethodPatch, "/agents/{agent_id}", "Change an Agent's name or system prompt", 0, c.UpdateAgent)
	register(h, "suspend-agent", http.MethodPost, "/agents/{agent_id}/suspend", "Suspend an Agent", 0, c.SuspendAgent)
	register(h, "reactivate-agent", http.MethodPost, "/agents/{agent_id}/reactivate", "Reactivate an Agent", 0, c.ReactivateAgent)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"agents"}, DefaultStatus: status, Security: bearer,
	}, handler)
}

type ListAgentsRequest struct {
	Search          string `query:"q" maxLength:"255" doc:"Keep Agents whose name or slug contains this, ignoring case"`
	IncludeInactive bool   `query:"include_inactive" doc:"Also list suspended and deactivated Agents"`
	Limit           int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy          string `query:"sort_by" enum:"display_name,slug,created_at" doc:"Field to sort by: display_name (the default), slug, or created_at"`
	SortOrder       string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor      string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor  string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListAgentsRequest) query(organizationID string) orchestration.ListAgentsQuery {
	page := pagination.Query{Limit: r.Limit, SortBy: r.SortBy, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return orchestration.ListAgentsQuery{
		OrganizationID: organizationID, Search: r.Search,
		IncludeInactive: r.IncludeInactive, Page: page,
	}
}

type CreateAgentAPIRequest struct {
	Body struct {
		DisplayName  string  `json:"display_name" required:"true" minLength:"1" maxLength:"255" doc:"The display name for the Agent"`
		Slug         *string `json:"slug,omitempty" required:"false" doc:"Slug for the Agent, unique among the Organization's Agents; derived from the display name when omitted"`
		TeamID       string  `json:"team_id" required:"true" doc:"The active Team the Agent joins first"`
		Role         *string `json:"role,omitempty" required:"false" enum:"lead,member" doc:"The Agent's role on the Team; member when omitted"`
		SystemPrompt *string `json:"system_prompt,omitempty" required:"false" maxLength:"65536" doc:"Instructions given to the Agent for every Session"`
	}
}

func (r *CreateAgentAPIRequest) input(organizationID string) orchestration.CreateAgentInput {
	in := orchestration.CreateAgentInput{
		OrganizationID: organizationID, TeamID: r.Body.TeamID, DisplayName: r.Body.DisplayName,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	if r.Body.Role != nil {
		in.Role = team.Role(*r.Body.Role)
	}
	if r.Body.SystemPrompt != nil {
		in.SystemPrompt = *r.Body.SystemPrompt
	}
	return in
}

type AgentRequest struct {
	AgentID string `path:"agent_id" doc:"The Agent's principal ID"`
}

type UpdateAgentRequest struct {
	AgentID string `path:"agent_id" doc:"The Agent's principal ID"`
	Body    struct {
		DisplayName  *string `json:"display_name,omitempty" required:"false" minLength:"1" maxLength:"255" doc:"New display name"`
		SystemPrompt *string `json:"system_prompt,omitempty" required:"false" maxLength:"65536" doc:"New system prompt; an empty string clears it"`
	}
}

func (r *UpdateAgentRequest) props() orchestration.UpdateAgentProps {
	return orchestration.UpdateAgentProps{DisplayName: r.Body.DisplayName, SystemPrompt: r.Body.SystemPrompt}
}

type AgentResponse struct {
	Body api.Response[orchestration.AgentDetail]
}

type AgentListResponse struct {
	Body api.Response[pagination.Result[orchestration.AgentSummary]]
}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) CreateAgent(ctx context.Context, req *CreateAgentAPIRequest) (*AgentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := c.service.CreateAgent(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AgentResponse{Body: api.SuccessResponse(nil, *agent)}, nil
}

func (c *Controller) ListAgents(ctx context.Context, req *ListAgentsRequest) (*AgentListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListAgents(ctx, who.Actor, req.query(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AgentListResponse{Body: api.SuccessResponse(nil, *page)}, nil
}

func (c *Controller) GetAgent(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	return c.agent(ctx, req.AgentID, c.service.GetAgent)
}

func (c *Controller) UpdateAgent(ctx context.Context, req *UpdateAgentRequest) (*AgentResponse, error) {
	return c.agent(ctx, req.AgentID, func(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
		return c.service.UpdateAgent(ctx, actor, organizationID, agentID, req.props())
	})
}

func (c *Controller) SuspendAgent(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	return c.agent(ctx, req.AgentID, c.service.SuspendAgent)
}

func (c *Controller) ReactivateAgent(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	return c.agent(ctx, req.AgentID, c.service.ReactivateAgent)
}

func (c *Controller) agent(ctx context.Context, agentID string, op func(context.Context, identity.Actor, string, string) (*orchestration.AgentDetail, error)) (*AgentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := op(ctx, who.Actor, who.OrganizationID, agentID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AgentResponse{Body: api.SuccessResponse(nil, *agent)}, nil
}

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	var orchestrationInvalid *orchestration.ErrInvalidInput
	var teamInvalid *team.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &orchestrationInvalid):
		return huma.Error422UnprocessableEntity(orchestrationInvalid.Message)
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.As(err, &teamInvalid):
		return huma.Error422UnprocessableEntity(teamInvalid.Message)
	case errors.Is(err, iam.ErrInvalid), errors.Is(err, orchestration.ErrInvalid), errors.Is(err, team.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, iam.ErrNotFound), errors.Is(err, orchestration.ErrNotFound), errors.Is(err, team.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, iam.ErrAlreadyExists), errors.Is(err, iam.ErrStatus),
		errors.Is(err, team.ErrSlugTaken), errors.Is(err, team.ErrLastTeam), errors.Is(err, team.ErrArchived):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
