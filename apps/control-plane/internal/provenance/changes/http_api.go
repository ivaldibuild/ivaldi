package changes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the Change operations. Every operation requires an
// access token; the caller's Organization comes from the token.
type Controller struct {
	service provenance.ChangeService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service provenance.ChangeService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "record-change", http.MethodPost, "/sources/{source_id}/changes", "Record a Change that ties commits to a Session or Work Item", http.StatusCreated, c.RecordChange)
	register(h, "list-source-changes", http.MethodGet, "/sources/{source_id}/changes", "List a Source's recorded Changes", 0, c.ListChanges)
	register(h, "get-change", http.MethodGet, "/changes/{change_id}", "Get a recorded Change", 0, c.GetChange)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"provenance"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *Controller) RecordChange(ctx context.Context, req *RecordChangeRequest) (*ChangeResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	change, err := c.service.RecordChange(ctx, who.Actor, provenance.RecordChangeInput{
		OrganizationID: who.OrganizationID,
		SourceID:       req.SourceID,
		SessionID:      req.Body.SessionID,
		WorkItemID:     req.Body.WorkItemID,
		Title:          req.Body.Title,
		ExternalRef:    req.Body.ExternalRef,
		CommitSHAs:     req.Body.CommitSHAs,
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return changeResponse(change), nil
}

func (c *Controller) ListChanges(ctx context.Context, req *ListChangesRequest) (*ChangeListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListChanges(ctx, who.Actor, provenance.ListChangesQuery{
		OrganizationID: who.OrganizationID,
		SourceID:       req.SourceID,
		Page:           req.PageParams.query(),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &ChangeListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page))
	return out, nil
}

func (c *Controller) GetChange(ctx context.Context, req *ChangeRequest) (*ChangeResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	change, err := c.service.GetChange(ctx, who.Actor, who.OrganizationID, req.ChangeID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return changeResponse(change), nil
}
