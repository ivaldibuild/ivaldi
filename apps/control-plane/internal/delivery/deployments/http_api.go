package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Controller serves Deployment operations for the caller's Organization.
type Controller struct {
	service delivery.DeploymentService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service delivery.DeploymentService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "request-deployments", http.MethodPost, "/releases/{release_id}/deployments",
		"Request execution of a published Release on one or more Targets", http.StatusCreated, c.RequestDeployments)
	register(h, "list-deployments", http.MethodGet, "/applications/{application}/deployments",
		"List an Application's Deployments", 0, c.ListDeployments)
	register(h, "get-deployment", http.MethodGet, "/deployments/{deployment_id}",
		"Get a Deployment with its event timeline", 0, c.GetDeployment)
	register(h, "cancel-deployment", http.MethodPost, "/deployments/{deployment_id}/cancel",
		"Cancel a Deployment before it is applied", 0, c.CancelDeployment)
	register(h, "release-changes", http.MethodGet, "/releases/{release_id}/changes",
		"Show what a Release would change on a Target, and its pre-flight checks", 0, c.ReleaseChanges)
	register(h, "list-target-assignments", http.MethodGet, "/targets/{target_id}/assignments",
		"List the active Deployment assignments for the Target's agent", 0, c.ListAssignments)
	register(h, "report-deployment", http.MethodPost, "/deployments/{deployment_id}/events",
		"Report a Deployment's progress or health as its Target's agent", 0, c.ReportDeployment)
	register(h, "report-target-heartbeat", http.MethodPost, "/targets/{target_id}/reports",
		"Note that the Target's agent is in contact", http.StatusNoContent, c.ReportTargetHeartbeat)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"deployments"}, DefaultStatus: status, Security: bearer,
	}, handler)
}

type RequestDeploymentsRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release to deploy"`
	Body      struct {
		TargetIDs []string `json:"target_ids" required:"true" minItems:"1" maxItems:"20" doc:"The Targets to deploy the Release to"`
	}
}

type ListDeploymentsRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID or slug"`
	ReleaseID             string `query:"release_id" doc:"Only Deployments of this Release"`
	TargetID              string `query:"target_id" doc:"Only Deployments to this Target"`
	Status                string `query:"status" enum:"pending,awaiting_approval,rejected,cancelled,applying,progressing,healthy,degraded,failed,superseded" doc:"Only Deployments with this status"`
	Limit                 int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy                string `query:"sort_by" enum:"requested_at,status" doc:"Field to sort by: requested_at (the default) or status"`
	SortOrder             string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor            string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor        string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListDeploymentsRequest) query(organizationID string) delivery.ListDeploymentsQuery {
	page := pagination.Query{Limit: r.Limit, SortBy: r.SortBy, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return delivery.ListDeploymentsQuery{
		OrganizationID: organizationID, ApplicationID: r.ApplicationIdentifier,
		ReleaseID: r.ReleaseID, TargetID: r.TargetID, Status: release.DeploymentStatus(r.Status), Page: page,
	}
}

type DeploymentRequest struct {
	DeploymentID string `path:"deployment_id" doc:"The Deployment"`
}

type ReleaseChangesRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release"`
	TargetID  string `query:"target_id" required:"true" doc:"The Target to compare against"`
}

type TargetRequest struct {
	TargetID string `path:"target_id" doc:"The Target"`
}

type ReportDeploymentRequest struct {
	DeploymentID string `path:"deployment_id" doc:"The Deployment"`
	Body         struct {
		ReportID   string          `json:"report_id" required:"true" minLength:"1" maxLength:"255" doc:"The agent's idempotent report ID"`
		Kind       string          `json:"kind" required:"true" enum:"status_changed,progress,health" doc:"What the report describes"`
		Status     string          `json:"status,omitempty" required:"false" enum:"progressing,healthy,degraded,failed" doc:"The Deployment status to move to, when the report changes it"`
		Detail     json.RawMessage `json:"detail,omitempty" required:"false" doc:"Runtime-specific detail"`
		OccurredAt *time.Time      `json:"occurred_at,omitempty" required:"false" doc:"When it happened, by the agent's clock"`
	}
}

type DeploymentsResponse struct {
	Body api.Response[[]delivery.DeploymentSnapshot]
}

type DeploymentResponse struct {
	Body api.Response[delivery.DeploymentDetail]
}

type DeploymentListResponse struct {
	Body api.Response[pagination.Result[delivery.DeploymentSnapshot]]
}

type AssignmentsResponse struct {
	Body api.Response[[]delivery.DeploymentAssignment]
}

type ChangesResponse struct {
	Body api.Response[delivery.ReleaseChanges]
}

type HeartbeatResponse struct{}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) RequestDeployments(ctx context.Context, req *RequestDeploymentsRequest) (*DeploymentsResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.service.RequestDeployments(ctx, who.Actor, delivery.RequestDeploymentsInput{
		OrganizationID: who.OrganizationID, ReleaseID: req.ReleaseID, TargetIDs: req.Body.TargetIDs,
		SessionID: optional.Of(who.Actor.SessionID),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := make([]delivery.DeploymentSnapshot, len(items))
	for i, d := range items {
		out[i] = d.Snapshot()
	}
	return &DeploymentsResponse{Body: api.SuccessResponse(nil, out)}, nil
}

func (c *Controller) ListDeployments(ctx context.Context, req *ListDeploymentsRequest) (*DeploymentListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListDeployments(ctx, who.Actor, req.query(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &DeploymentListResponse{Body: api.SuccessResponse(nil, *page)}, nil
}

func (c *Controller) GetDeployment(ctx context.Context, req *DeploymentRequest) (*DeploymentResponse, error) {
	return c.deployment(ctx, req.DeploymentID, c.service.GetDeployment)
}

func (c *Controller) CancelDeployment(ctx context.Context, req *DeploymentRequest) (*DeploymentResponse, error) {
	return c.deployment(ctx, req.DeploymentID, c.service.CancelDeployment)
}

func (c *Controller) deployment(ctx context.Context, deploymentID string,
	op func(context.Context, identity.Actor, string, string) (*delivery.DeploymentDetail, error)) (*DeploymentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	d, err := op(ctx, who.Actor, who.OrganizationID, deploymentID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &DeploymentResponse{Body: api.SuccessResponse(nil, *d)}, nil
}

func (c *Controller) ReleaseChanges(ctx context.Context, req *ReleaseChangesRequest) (*ChangesResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	changes, err := c.service.ReleaseChanges(ctx, who.Actor, who.OrganizationID, req.ReleaseID, req.TargetID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &ChangesResponse{Body: api.SuccessResponse(nil, *changes)}, nil
}

func (c *Controller) ListAssignments(ctx context.Context, req *TargetRequest) (*AssignmentsResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.service.ListAssignments(ctx, who.Actor, who.OrganizationID, req.TargetID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AssignmentsResponse{Body: api.SuccessResponse(nil, items)}, nil
}

func (c *Controller) ReportDeployment(ctx context.Context, req *ReportDeploymentRequest) (*DeploymentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	in := delivery.ReportDeploymentInput{
		OrganizationID: who.OrganizationID, DeploymentID: req.DeploymentID, ReportID: req.Body.ReportID,
		Kind: delivery.DeploymentEventKind(req.Body.Kind), Detail: req.Body.Detail, OccurredAt: req.Body.OccurredAt,
	}
	if req.Body.Status != "" {
		status := release.DeploymentStatus(req.Body.Status)
		in.Status = &status
	}
	d, err := c.service.ReportDeployment(ctx, who.Actor, in)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &DeploymentResponse{Body: api.SuccessResponse(nil, *d)}, nil
}

func (c *Controller) ReportTargetHeartbeat(ctx context.Context, req *TargetRequest) (*HeartbeatResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.RecordTargetHeartbeat(ctx, who.Actor, who.OrganizationID, req.TargetID); err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &HeartbeatResponse{}, nil
}

func translateToHttpError(ctx context.Context, err error) error {
	var invalid *delivery.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, delivery.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, delivery.ErrDeploymentInFlight):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, delivery.ErrDeploymentStatus), errors.Is(err, delivery.ErrReleaseStatus),
		errors.Is(err, delivery.ErrArchived), errors.Is(err, delivery.ErrConflict),
		errors.Is(err, delivery.ErrAlreadyExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, delivery.ErrNotDeploymentAgent):
		return huma.Error403Forbidden(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
