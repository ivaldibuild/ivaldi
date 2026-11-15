package releases

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// ReleaseController serves Builds and Releases. Every operation requires an
// access token; the caller's Organization comes from the token, never from
// the request.
type ReleaseController struct {
	service delivery.ReleaseService
}

var _ api.Routes = (*ReleaseController)(nil)

func NewReleaseController(service delivery.ReleaseService) *ReleaseController {
	return &ReleaseController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *ReleaseController) RegisterRoutes(h huma.API) {
	register(h, "report-build", http.MethodPost, "/applications/{application}/builds",
		"Record a Build of one of an Application's artifacts, as CI reports it", 0, c.ReportBuild)
	register(h, "create-release", http.MethodPost, "/applications/{application}/releases",
		"Create a Release from its Builds, and publish it if asked", http.StatusCreated, c.CreateRelease)
	register(h, "list-releases", http.MethodGet, "/applications/{application}/releases",
		"List an Application's Releases", 0, c.ListReleases)
	register(h, "get-release", http.MethodGet, "/releases/{release_id}", "Get a Release with its Builds", 0, c.GetRelease)
	register(h, "get-release-variables", http.MethodGet, "/releases/{release_id}/variables",
		"Get the variables a Release deploys with, and what a Target would get", 0, c.GetReleaseVariables)
	register(h, "refresh-release-variables", http.MethodPost, "/releases/{release_id}/variables/refresh",
		"Take a new revision of a Release's variables from its Application's, or preview what would change", 0, c.RefreshReleaseVariables)
	register(h, "publish-release", http.MethodPost, "/releases/{release_id}/publish",
		"Publish a draft Release that meets its Channel's rules", 0, c.PublishRelease)
	register(h, "withdraw-release", http.MethodPost, "/releases/{release_id}/withdraw",
		"Withdraw a published Release so it is not deployed again", 0, c.WithdrawRelease)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"releases"},
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

func (c *ReleaseController) ReportBuild(ctx context.Context, req *ReportBuildRequest) (*BuildResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	b, err := c.service.ReportBuild(ctx, who.Actor, delivery.ReportBuildInput{
		OrganizationID: who.OrganizationID, ApplicationID: req.ApplicationIdentifier, Build: req.Body.input(),
	})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &BuildResponse{Body: api.SuccessResponse(nil, b.Snapshot())}, nil
}

func (c *ReleaseController) CreateRelease(ctx context.Context, req *CreateReleaseRequest) (*ReleaseResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	in := delivery.CreateReleaseInput{
		OrganizationID:   who.OrganizationID,
		ApplicationID:    req.ApplicationIdentifier,
		Version:          req.Body.Version,
		ChannelID:        req.Body.ChannelID,
		Publish:          req.Body.Publish,
		TriggeredBy:      req.Body.TriggeredBy,
		TriggeredByEmail: req.Body.TriggeredByEmail,
	}

	for _, b := range req.Body.Builds {
		in.Builds = append(in.Builds, b.input())
	}

	return c.respond(ctx, func() (*delivery.ReleaseDetail, error) { return c.service.CreateRelease(ctx, who.Actor, in) })
}

func (c *ReleaseController) ListReleases(ctx context.Context, req *ListReleasesRequest) (*ReleaseListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page := pagination.Query{Limit: req.Limit, SortBy: req.SortBy, SortOrder: req.SortOrder}
	if req.NextCursor != "" {
		page.NextCursor = &req.NextCursor
	}
	if req.PreviousCursor != "" {
		page.PreviousCursor = &req.PreviousCursor
	}
	result, err := c.service.ListReleases(ctx, who.Actor, delivery.ListReleasesQuery{
		OrganizationID: who.OrganizationID, ApplicationID: req.ApplicationIdentifier,
		ChannelID: req.ChannelID, Status: release.Status(req.Status), Page: page,
	})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	items := make([]Release, len(result.Items))
	for i, d := range result.Items {
		items[i] = releaseView(d)
	}
	return &ReleaseListResponse{Body: api.SuccessResponse(nil, pagination.Result[Release]{
		Items: items, TotalCount: result.TotalCount, HasNext: result.HasNext, HasPrevious: result.HasPrevious,
		NextCursor: result.NextCursor, PreviousCursor: result.PreviousCursor,
	})}, nil
}

func (c *ReleaseController) GetRelease(ctx context.Context, req *ReleaseRequest) (*ReleaseResponse, error) {
	return c.onRelease(ctx, req.ReleaseID, c.service.GetRelease)
}

func (c *ReleaseController) GetReleaseVariables(ctx context.Context, req *ReleaseVariablesRequest) (*ReleaseVariablesResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := c.service.GetReleaseVariables(ctx, who.Actor, who.OrganizationID, req.ReleaseID, req.TargetID, req.DeploymentID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ReleaseVariablesResponse{Body: api.SuccessResponse(nil, *view)}, nil
}

func (c *ReleaseController) RefreshReleaseVariables(ctx context.Context, req *RefreshReleaseVariablesRequest) (*ReleaseVariablesRefreshResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	refresh, err := c.service.RefreshReleaseVariables(ctx, who.Actor, who.OrganizationID, req.ReleaseID, req.Preview)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ReleaseVariablesRefreshResponse{Body: api.SuccessResponse(nil, *refresh)}, nil
}

func (c *ReleaseController) PublishRelease(ctx context.Context, req *ReleaseRequest) (*ReleaseResponse, error) {
	return c.onRelease(ctx, req.ReleaseID, c.service.PublishRelease)
}

func (c *ReleaseController) WithdrawRelease(ctx context.Context, req *ReleaseRequest) (*ReleaseResponse, error) {
	return c.onRelease(ctx, req.ReleaseID, c.service.WithdrawRelease)
}

func (c *ReleaseController) onRelease(ctx context.Context, releaseID string,
	op func(context.Context, identity.Actor, string, string) (*delivery.ReleaseDetail, error)) (*ReleaseResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.respond(ctx, func() (*delivery.ReleaseDetail, error) { return op(ctx, who.Actor, who.OrganizationID, releaseID) })
}

func (c *ReleaseController) respond(ctx context.Context, op func() (*delivery.ReleaseDetail, error)) (*ReleaseResponse, error) {
	d, err := op()
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &ReleaseResponse{Body: api.SuccessResponse(nil, releaseView(d))}, nil
}

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail, since its
// message may describe internals such as SQL.
func httpError(ctx context.Context, err error) error {
	var invalid *delivery.ErrInvalidInput
	var rules *delivery.RulesError
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &rules):
		return huma.Error422UnprocessableEntity(rules.Error())
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrTagsUnsupported):
		return huma.Error422UnprocessableEntity(delivery.ErrTagsUnsupported.Error())
	case errors.Is(err, delivery.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, delivery.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, delivery.ErrAmbiguous):
		return huma.Error409Conflict("more than one active Application has that slug; use its ID")
	case errors.Is(err, delivery.ErrAlreadyExists),
		errors.Is(err, delivery.ErrArchived),
		errors.Is(err, delivery.ErrReleaseStatus),
		errors.Is(err, delivery.ErrProductionChange),
		errors.Is(err, delivery.ErrBuildConflict):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
