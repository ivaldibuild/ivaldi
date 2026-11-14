package clusters

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves the Cluster operations. Every operation requires an
// access token; the caller's Organization comes from the token, never from
// the request.
type Controller struct {
	service delivery.ClusterService
}

var _ api.Routes = (*Controller)(nil)

func NewController(service delivery.ClusterService) *Controller {
	return &Controller{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-cluster", http.MethodPost, "/clusters", "Create a Cluster", http.StatusCreated, c.CreateCluster)
	register(h, "list-clusters", http.MethodGet, "/clusters", "List the Organization's Clusters", 0, c.ListClusters)
	register(h, "get-cluster", http.MethodGet, "/clusters/{cluster_id}", "Get a Cluster", 0, c.GetCluster)
	register(h, "update-cluster", http.MethodPatch, "/clusters/{cluster_id}", "Change a Cluster's display name", 0, c.UpdateCluster)
	register(h, "archive-cluster", http.MethodPost, "/clusters/{cluster_id}/archive", "Archive a Cluster and revoke its agent", 0, c.ArchiveCluster)
	register(h, "restore-cluster", http.MethodPost, "/clusters/{cluster_id}/restore", "Restore an archived Cluster", 0, c.RestoreCluster)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"clusters"},
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

func (c *Controller) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	cluster, err := c.service.CreateCluster(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return clusterResponse(cluster), nil
}

func (c *Controller) ListClusters(ctx context.Context, req *ListClustersRequest) (*ClusterListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListClusters(ctx, who.Actor, delivery.ListClustersQuery{
		OrganizationID:  who.OrganizationID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	out := &ClusterListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*delivery.Cluster).Snapshot))
	return out, nil
}

func (c *Controller) GetCluster(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error) {
	return c.cluster(ctx, req.ClusterID, c.service.GetCluster)
}

func (c *Controller) UpdateCluster(ctx context.Context, req *UpdateClusterRequest) (*ClusterResponse, error) {
	return c.cluster(ctx, req.ClusterID, func(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
		return c.service.UpdateCluster(ctx, actor, organizationID, clusterID, req.props())
	})
}

func (c *Controller) ArchiveCluster(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error) {
	return c.cluster(ctx, req.ClusterID, c.service.ArchiveCluster)
}

func (c *Controller) RestoreCluster(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error) {
	return c.cluster(ctx, req.ClusterID, c.service.RestoreCluster)
}

// cluster calls an operation on one Cluster.
func (c *Controller) cluster(ctx context.Context, clusterID string, op func(context.Context, identity.Actor, string, string) (*delivery.Cluster, error)) (*ClusterResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	cluster, err := op(ctx, who.Actor, who.OrganizationID, clusterID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return clusterResponse(cluster), nil
}
