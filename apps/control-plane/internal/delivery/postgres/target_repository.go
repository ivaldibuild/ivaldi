package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type targetRepository struct {
	db     *gorm.DB
	mapper *targetMapper
}

// NewTargetRepository returns the PostgreSQL TargetRepository. It uses the
// transaction carried by the context when there is one.
func NewTargetRepository(db *gorm.DB) delivery.TargetRepository {
	return &targetRepository{db: db, mapper: newTargetMapper()}
}

func (r *targetRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *targetRepository) CreateTarget(ctx context.Context, t *delivery.Target) error {
	e, err := r.mapper.ToPersistence(t)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *targetRepository) GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*delivery.Target, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, targetID))
}

func (r *targetRepository) GetTargetByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*delivery.Target, error) {
	return r.get(r.conn(ctx).Where(
		"organization_id = ? AND agent_principal_id = ? AND deleted_at IS NULL", organizationID, principalID))
}

func (r *targetRepository) GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, targetID))
}

func (r *targetRepository) GetTargetBySlug(ctx context.Context, organizationID, environmentID, slug string) (*delivery.Target, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND environment_id = ? AND slug = ? AND deleted_at IS NULL", organizationID, environmentID, slug))
}

func (r *targetRepository) get(q *gorm.DB) (*delivery.Target, error) {
	var entity targetEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *targetRepository) ListTargets(ctx context.Context, query delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&targetEntity{}).Where("organization_id = ? AND environment_id = ?", query.OrganizationID, query.EnvironmentID)
		if !query.IncludeArchived {
			q = q.Where("deleted_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, targetKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, r.mapper.ToDomain)
}

func (r *targetRepository) UpdateTarget(ctx context.Context, t *delivery.Target) error {
	e, err := r.mapper.ToPersistence(t)
	if err != nil {
		return err
	}
	return updated(r.conn(ctx).Model(&targetEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"slug":               e.Slug,
			"display_name":       e.DisplayName,
			"locator":            e.Locator,
			"agent_principal_id": e.AgentPrincipalID,
			"last_reported_at":   e.LastReportedAt,
			"updated_at":         e.UpdatedAt,
			"deleted_at":         e.DeletedAt,
		}))
}

func (r *targetRepository) CountActiveTargets(ctx context.Context, organizationID, environmentID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&targetEntity{}).
		Where("organization_id = ? AND environment_id = ? AND deleted_at IS NULL", organizationID, environmentID).
		Count(&n).Error
	return int(n), translate(err)
}
