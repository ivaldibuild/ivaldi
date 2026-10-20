// Package postgres implements the delivery repositories with GORM on the
// delivery schema created by ../migrations.
package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type environmentRepository struct {
	db     *gorm.DB
	mapper *environmentMapper
}

// NewEnvironmentRepository returns the PostgreSQL EnvironmentRepository. It
// uses the transaction carried by the context when there is one.
func NewEnvironmentRepository(db *gorm.DB) delivery.EnvironmentRepository {
	return &environmentRepository{
		db:     db,
		mapper: newEnvironmentMapper(),
	}
}

func (r *environmentRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *environmentRepository) CreateEnvironment(ctx context.Context, env *delivery.Environment) error {
	return translate(r.conn(ctx).Create(r.mapper.ToPersistence(env)).Error)
}

func (r *environmentRepository) GetEnvironment(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, environmentID))
}

func (r *environmentRepository) GetEnvironmentForUpdate(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, environmentID))
}

func (r *environmentRepository) GetEnvironmentBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*delivery.Environment, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND workspace_id = ? AND slug = ? AND deleted_at IS NULL", organizationID, workspaceID, slug))
}

func (r *environmentRepository) get(q *gorm.DB) (*delivery.Environment, error) {
	var entity environmentEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *environmentRepository) GetEnvironments(ctx context.Context, organizationID string, environmentIDs []string) ([]*delivery.Environment, error) {
	if len(environmentIDs) == 0 {
		return nil, nil
	}
	var entities []environmentEntity
	err := r.conn(ctx).Where("organization_id = ? AND id IN ?", organizationID, environmentIDs).Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	environments := make([]*delivery.Environment, 0, len(entities))
	for i := range entities {
		env, err := r.mapper.ToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}
	return environments, nil
}

func (r *environmentRepository) ListEnvironments(ctx context.Context, query delivery.ListEnvironmentsQuery) (*pagination.Result[*delivery.Environment], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&environmentEntity{}).Where("organization_id = ? AND workspace_id = ?", query.OrganizationID, query.WorkspaceID)
		if !query.IncludeArchived {
			q = q.Where("deleted_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, environmentKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, r.mapper.ToDomain)
}

func (r *environmentRepository) UpdateEnvironment(ctx context.Context, env *delivery.Environment) error {
	e := r.mapper.ToPersistence(env)
	return updated(r.conn(ctx).Model(&environmentEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"slug":              e.Slug,
			"display_name":      e.DisplayName,
			"tier":              e.Tier,
			"execution_enabled": e.ExecutionEnabled,
			"updated_at":        e.UpdatedAt,
			"deleted_at":        e.DeletedAt,
		}))
}

func (r *environmentRepository) CountActiveEnvironments(ctx context.Context, organizationID, workspaceID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&environmentEntity{}).
		Where("organization_id = ? AND workspace_id = ? AND deleted_at IS NULL", organizationID, workspaceID).
		Count(&n).Error
	return int(n), translate(err)
}
