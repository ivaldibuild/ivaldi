package clusters

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// A Cluster belongs to the Organization rather than a Workspace, so every
// check is on the Organization: reading needs cluster.read, and creating,
// changing, archiving, and restoring need cluster.manage.
type authorized struct {
	next  delivery.ClusterService
	authz authorization.Authorizer
}

var _ delivery.ClusterService = (*authorized)(nil)

func (a *authorized) allow(ctx context.Context, subject string, req authorization.Request) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, req)
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, subject, d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, action string) error {
	_, err := a.allow(ctx, "", authorization.Request{
		Actor: actor, Permission: delivery.PermissionClusterRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action,
	})
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, "", authorization.Request{
		Actor: actor, Permission: delivery.PermissionClusterManage, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action, Proposal: proposal,
	})
}

func (a *authorized) CreateCluster(ctx context.Context, actor identity.Actor, in delivery.CreateClusterInput) (*delivery.Cluster, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, delivery.ActionClusterCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateCluster(ctx, actor, in)
}

func (a *authorized) GetCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	if err := a.read(ctx, actor, organizationID, delivery.ActionClusterRead); err != nil {
		return nil, err
	}
	return a.next.GetCluster(ctx, actor, organizationID, clusterID)
}

func (a *authorized) ListClusters(ctx context.Context, actor identity.Actor, query delivery.ListClustersQuery) (*pagination.Result[*delivery.Cluster], error) {
	if err := a.read(ctx, actor, query.OrganizationID, delivery.ActionClusterList); err != nil {
		return nil, err
	}
	return a.next.ListClusters(ctx, actor, query)
}

func (a *authorized) UpdateCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string, props delivery.UpdateClusterProps) (*delivery.Cluster, error) {
	ctx, err := a.manage(ctx, actor, organizationID, delivery.ActionClusterUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateCluster(ctx, actor, organizationID, clusterID, props)
}

func (a *authorized) ArchiveCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	ctx, err := a.manage(ctx, actor, organizationID, delivery.ActionClusterArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveCluster(ctx, actor, organizationID, clusterID)
}

func (a *authorized) RestoreCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	ctx, err := a.manage(ctx, actor, organizationID, delivery.ActionClusterRestore, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreCluster(ctx, actor, organizationID, clusterID)
}
