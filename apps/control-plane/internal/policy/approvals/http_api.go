package approvals

import (
	"context"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the approval request operations for the caller's
// Organization.
type Controller struct {
	service   policy.ApprovalService
	summaries []any
}

var _ api.Routes = (*Controller)(nil)

// NewController serves service. summaries are zero values of every
// registered subject type's summary, such as
// delivery.DeploymentApprovalSummary{}, which the API's description lists.
func NewController(service policy.ApprovalService, summaries ...any) *Controller {
	return &Controller{service: service, summaries: summaries}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "list-approvals", http.MethodGet, "/approvals",
		"List the approval requests you could decide", 0, c.ListApprovals)
	register(h, "get-approval", http.MethodGet, "/approvals/{approval_id}",
		"Get an approval request, and whether you could decide it", 0, c.GetApproval)
	register(h, "approve-approval", http.MethodPost, "/approvals/{approval_id}/approve",
		"Approve a pending request, which applies what waits for it", 0, c.Approve)
	register(h, "reject-approval", http.MethodPost, "/approvals/{approval_id}/reject",
		"Reject a pending request, saying why", 0, c.Reject)
	c.describeSummaries(h.OpenAPI().Components.Schemas)
}

// describeSummaries makes the summary property one of the subject types'
// summaries, so clients know their fields. Policy cannot name those types
// itself: the capabilities that own the subjects define them.
func (c *Controller) describeSummaries(registry huma.Registry) {
	view := registry.Schema(reflect.TypeFor[policy.ApprovalRequestView](), false, "")
	summary := &huma.Schema{Description: "What approvers are shown, as the subject's capability described it"}
	for _, s := range c.summaries {
		summary.OneOf = append(summary.OneOf, registry.Schema(reflect.TypeOf(s), true, ""))
	}
	view.Properties["summary"] = summary
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"approvals"}, DefaultStatus: status, Security: bearer,
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

func (c *Controller) ListApprovals(ctx context.Context, req *ListApprovalsRequest) (*ApprovalListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListApprovalRequests(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ApprovalListResponse{Body: api.SuccessResponse(nil, *page)}, nil
}

func (c *Controller) GetApproval(ctx context.Context, req *ApprovalIDRequest) (*ApprovalResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := c.service.GetApprovalRequest(ctx, who.Actor, who.OrganizationID, req.ApprovalID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ApprovalResponse{Body: api.SuccessResponse(nil, *view)}, nil
}

func (c *Controller) Approve(ctx context.Context, req *ApproveRequest) (*ApprovalResponse, error) {
	return c.decide(ctx, req.ApprovalID, req.Body.Note, c.service.ApproveRequest)
}

func (c *Controller) Reject(ctx context.Context, req *RejectRequest) (*ApprovalResponse, error) {
	return c.decide(ctx, req.ApprovalID, req.Body.Note, c.service.RejectRequest)
}

func (c *Controller) decide(ctx context.Context, requestID, note string,
	op func(context.Context, identity.Actor, policy.DecideApprovalInput) (*policy.ApprovalRequestView, error)) (*ApprovalResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := op(ctx, who.Actor, policy.DecideApprovalInput{OrganizationID: who.OrganizationID, RequestID: requestID, Note: note})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ApprovalResponse{Body: api.SuccessResponse(nil, *view)}, nil
}
