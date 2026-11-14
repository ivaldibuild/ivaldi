package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type clusterRepository struct {
	db     *gorm.DB
	mapper *clusterMapper
}

// NewClusterRepository returns the PostgreSQL ClusterRepository. It uses the
// transaction carried by the context when there is one.
func NewClusterRepository(db *gorm.DB) delivery.ClusterRepository {
	return &clusterRepository{db: db, mapper: newClusterMapper()}
}

func (r *clusterRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *clusterRepository) CreateCluster(ctx context.Context, c *delivery.Cluster) error {
	e, err := r.mapper.ToPersistence(c)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *clusterRepository) GetCluster(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, clusterID))
}

func (r *clusterRepository) GetClusterForUpdate(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, clusterID))
}

func (r *clusterRepository) GetClusterForShare(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthShare}).
		Where("organization_id = ? AND id = ?", organizationID, clusterID))
}

func (r *clusterRepository) GetClusterBySlug(ctx context.Context, organizationID, slug string) (*delivery.Cluster, error) {
	return r.get(r.conn(ctx).Where(
		"organization_id = ? AND slug = ? AND deleted_at IS NULL", organizationID, slug))
}

func (r *clusterRepository) GetClusterByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*delivery.Cluster, error) {
	return r.get(r.conn(ctx).Where(
		"organization_id = ? AND agent_principal_id = ? AND deleted_at IS NULL", organizationID, principalID))
}

func (r *clusterRepository) get(q *gorm.DB) (*delivery.Cluster, error) {
	var entity clusterEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *clusterRepository) ListClusters(ctx context.Context, query delivery.ListClustersQuery) (*pagination.Result[*delivery.Cluster], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&clusterEntity{}).Where("organization_id = ?", query.OrganizationID)
		if !query.IncludeArchived {
			q = q.Where("deleted_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, clusterKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, r.mapper.ToDomain)
}

func (r *clusterRepository) UpdateCluster(ctx context.Context, c *delivery.Cluster) error {
	e, err := r.mapper.ToPersistence(c)
	if err != nil {
		return err
	}
	return updated(r.conn(ctx).Model(&clusterEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"slug":               e.Slug,
			"display_name":       e.DisplayName,
			"runtime":            e.Runtime,
			"agent_principal_id": e.AgentPrincipalID,
			"agent_capabilities": e.AgentCapabilities,
			"agent_last_failure": e.AgentLastFailure,
			"last_reported_at":   e.LastReportedAt,
			"updated_at":         e.UpdatedAt,
			"deleted_at":         e.DeletedAt,
		}))
}

func (r *clusterRepository) CountActiveTargets(ctx context.Context, organizationID, clusterID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&targetEntity{}).
		Where("organization_id = ? AND cluster_id = ? AND deleted_at IS NULL", organizationID, clusterID).
		Count(&n).Error
	return int(n), translate(err)
}
