package connections

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the Connection operations. Every operation requires an
// access token; the caller's Organization comes from the token, never from
// the request.
type Controller struct {
	service versioncontrol.ConnectionService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service versioncontrol.ConnectionService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-connection", http.MethodPost, "/connections", "Connect a version-control provider account", http.StatusCreated, c.CreateConnection)
	register(h, "list-connections", http.MethodGet, "/connections", "List the Organization's version-control connections", 0, c.ListConnections)
	register(h, "get-connection", http.MethodGet, "/connections/{connection_id}", "Get a version-control connection", 0, c.GetConnection)
	register(h, "archive-connection", http.MethodPost, "/connections/{connection_id}/archive", "Archive a version-control connection", 0, c.ArchiveConnection)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"version-control"},
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

func (c *Controller) CreateConnection(ctx context.Context, req *CreateConnectionRequest) (*ConnectionResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := c.service.CreateConnection(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return connectionResponse(connection), nil
}

func (c *Controller) ListConnections(ctx context.Context, req *ListConnectionsRequest) (*ConnectionListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.service.ListConnections(ctx, who.Actor, who.OrganizationID, req.IncludeArchived)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &ConnectionListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(items))
	return out, nil
}

func (c *Controller) GetConnection(ctx context.Context, req *ConnectionRequest) (*ConnectionResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := c.service.GetConnection(ctx, who.Actor, who.OrganizationID, req.ConnectionID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return connectionResponse(connection), nil
}

func (c *Controller) ArchiveConnection(ctx context.Context, req *ConnectionRequest) (*ConnectionResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := c.service.ArchiveConnection(ctx, who.Actor, who.OrganizationID, req.ConnectionID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return connectionResponse(connection), nil
}
