package connections

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
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
	register(h, "create-connection", http.MethodPost, "/connections", "Connect a version-control provider account that does not use an app installation", http.StatusCreated, c.CreateConnection)
	register(h, "start-connection-installation", http.MethodPost, "/connections/installations", "Start installing a provider's app to connect an account", 0, c.StartInstallation)
	register(h, "complete-connection-installation", http.MethodPost, "/connections/installations/complete", "Connect the account an app installation covers, once the provider confirms the caller can access it", http.StatusCreated, c.CompleteInstallation)
	register(h, "list-connections", http.MethodGet, "/connections", "List the Organization's version-control connections", 0, c.ListConnections)
	register(h, "get-connection", http.MethodGet, "/connections/{connection_id}", "Get a version-control connection", 0, c.GetConnection)
	register(h, "list-connection-repositories", http.MethodGet, "/connections/{connection_id}/repositories", "List the repositories a connection's account lets Ivaldi read", 0, c.ListRepositories)
	register(h, "archive-connection", http.MethodPost, "/connections/{connection_id}/archive", "Archive a version-control connection", 0, c.ArchiveConnection)
	register(h, "suspend-connection", http.MethodPost, "/connections/{connection_id}/suspend", "Mark a version-control connection unusable", 0, c.SuspendConnection)
	register(h, "revoke-connection", http.MethodPost, "/connections/{connection_id}/revoke", "Mark a version-control connection unusable for good", 0, c.RevokeConnection)
	register(h, "reactivate-connection", http.MethodPost, "/connections/{connection_id}/reactivate", "Make a suspended or revoked connection usable again", 0, c.ReactivateConnection)
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
	return c.connectionAction(ctx, req.ConnectionID, c.service.ArchiveConnection)
}

func (c *Controller) SuspendConnection(ctx context.Context, req *ConnectionRequest) (*ConnectionResponse, error) {
	return c.connectionAction(ctx, req.ConnectionID, c.service.SuspendConnection)
}

func (c *Controller) RevokeConnection(ctx context.Context, req *ConnectionRequest) (*ConnectionResponse, error) {
	return c.connectionAction(ctx, req.ConnectionID, c.service.RevokeConnection)
}

func (c *Controller) ReactivateConnection(ctx context.Context, req *ConnectionRequest) (*ConnectionResponse, error) {
	return c.connectionAction(ctx, req.ConnectionID, c.service.ReactivateConnection)
}

func (c *Controller) connectionAction(ctx context.Context, connectionID string, op func(context.Context, identity.Actor, string, string) (*versioncontrol.Connection, error)) (*ConnectionResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := op(ctx, who.Actor, who.OrganizationID, connectionID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return connectionResponse(connection), nil
}

func (c *Controller) StartInstallation(ctx context.Context, req *StartInstallationRequest) (*InstallationStartResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	start, err := c.service.StartInstallation(ctx, who.Actor, who.OrganizationID, vcs.ProviderKind(req.Body.Provider))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &InstallationStartResponse{Body: api.SuccessResponse(nil, InstallationStartSnapshot{
		URL: start.URL, ExpiresAt: start.ExpiresAt,
	})}, nil
}

func (c *Controller) CompleteInstallation(ctx context.Context, req *CompleteInstallationRequest) (*ConnectionResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := c.service.CompleteInstallation(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return connectionResponse(connection), nil
}

func (c *Controller) ListRepositories(ctx context.Context, req *ConnectionRequest) (*RepositoryListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	repositories, err := c.service.ListRepositories(ctx, who.Actor, who.OrganizationID, req.ConnectionID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &RepositoryListResponse{}
	out.Body = api.SuccessResponse(nil, repositorySnapshots(repositories))
	return out, nil
}
