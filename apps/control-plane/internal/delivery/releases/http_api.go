package releases

import (
	"context"
	"errors"
	"net/http"
	"time"

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

// BuildBody names one artifact's Build and carries what CI knows about it.
type BuildBody struct {
	Artifact string `json:"artifact" required:"true" minLength:"1" doc:"The name of the Application's artifact, such as api"`
	Digest   string `json:"digest,omitempty" required:"false" doc:"The content digest, such as sha256:<64 hex characters>"`
	Tag      string `json:"tag,omitempty" required:"false" doc:"A tag to resolve to a digest through the artifact's Registry; not supported yet, give the digest"`
	Version  string `json:"version,omitempty" required:"false" doc:"The version CI gave the Build"`
	SourceID string `json:"source_id,omitempty" required:"false" doc:"The Source it was built from"`
	// CommitSHA and SourceRef are what Channel rules and change history use.
	CommitSHA  string     `json:"commit_sha,omitempty" required:"false" doc:"The commit it was built from"`
	SourceRef  string     `json:"source_ref,omitempty" required:"false" doc:"The full Git ref it was built from, such as refs/tags/v1.4.0 or refs/pull/42/head"`
	CIProvider string     `json:"ci_provider,omitempty" required:"false" doc:"The CI system, such as github_actions"`
	CIRunRef   string     `json:"ci_run_ref,omitempty" required:"false" doc:"The CI run, such as its URL"`
	BuiltAt    *time.Time `json:"built_at,omitempty" required:"false" doc:"When it was built"`
	Tags       []string   `json:"tags,omitempty" required:"false" doc:"Tags pushed with the digest, as hints"`
}

func (b BuildBody) input() delivery.BuildInput {
	return delivery.BuildInput{
		Artifact: b.Artifact, Digest: b.Digest, Tag: b.Tag,
		Details: delivery.BuildDetails{
			Version: optional(b.Version), SourceID: optional(b.SourceID), CommitSHA: optional(b.CommitSHA),
			SourceRef: optional(b.SourceRef), CIProvider: optional(b.CIProvider), CIRunRef: optional(b.CIRunRef),
			ReportedTags: b.Tags, BuiltAt: b.BuiltAt,
		},
	}
}

type ReportBuildRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  BuildBody
}

type CreateReleaseRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		Version   string      `json:"version" required:"true" minLength:"1" maxLength:"255" doc:"The Release's version, unique within the Application, such as 1.4.0"`
		ChannelID string      `json:"channel_id,omitempty" required:"false" doc:"The Channel to join; when omitted, the one active Channel whose rules the Release meets"`
		Builds    []BuildBody `json:"builds" required:"true" minItems:"1" doc:"One Build per artifact"`
		Publish   bool        `json:"publish,omitempty" required:"false" doc:"Publish the Release as soon as it is created"`
	}
}

type ListReleasesRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	ChannelID             string `query:"channel_id" doc:"Keep the Releases in this Channel"`
	Status                string `query:"status" enum:"draft,published,withdrawn" doc:"Keep the Releases with this status"`
	Limit                 int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy                string `query:"sort_by" enum:"created_at,version" doc:"Field to sort by: created_at (the default) or version, as text"`
	SortOrder             string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor            string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor        string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

type ReleaseRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release"`
}

type BuildResponse struct {
	Body api.Response[delivery.BuildSnapshot]
}

type ReleaseResponse struct {
	Body api.Response[Release]
}

type ReleaseListResponse struct {
	Body api.Response[pagination.Result[Release]]
}

// Release is a Release with each of its Builds in full.
type Release struct {
	delivery.ReleaseSnapshot
	Builds []ReleaseBuild `json:"builds"`
}

// ReleaseBuild is the Build a Release carries for one artifact.
type ReleaseBuild struct {
	ArtifactID string                 `json:"artifact_id"`
	Build      delivery.BuildSnapshot `json:"build"`
}

func releaseView(d *delivery.ReleaseDetail) Release {
	out := Release{ReleaseSnapshot: d.Release.Snapshot(), Builds: []ReleaseBuild{}}
	for i, rb := range d.Release.Builds() {
		item := ReleaseBuild{ArtifactID: rb.ArtifactID}
		if i < len(d.Builds) && d.Builds[i] != nil {
			item.Build = d.Builds[i].Snapshot()
		}
		out.Builds = append(out.Builds, item)
	}
	return out
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
		OrganizationID: who.OrganizationID,
		ApplicationID:  req.ApplicationIdentifier,
		Version:        req.Body.Version,
		ChannelID:      req.Body.ChannelID,
		Publish:        req.Body.Publish,
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
		errors.Is(err, delivery.ErrBuildConflict):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
