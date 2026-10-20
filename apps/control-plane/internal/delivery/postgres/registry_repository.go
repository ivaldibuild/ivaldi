package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type registryRepository struct {
	db     *gorm.DB
	mapper *registryMapper
}

// NewRegistryRepository returns the PostgreSQL RegistryRepository. It uses
// the transaction carried by the context when there is one.
func NewRegistryRepository(db *gorm.DB) delivery.RegistryRepository {
	return &registryRepository{db: db, mapper: newRegistryMapper()}
}

func (r *registryRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *registryRepository) CreateRegistry(ctx context.Context, reg *delivery.Registry) error {
	return translate(r.conn(ctx).Create(r.mapper.ToPersistence(reg)).Error)
}

func (r *registryRepository) GetRegistry(ctx context.Context, organizationID, registryID string) (*delivery.Registry, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, registryID))
}

func (r *registryRepository) GetRegistryBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*delivery.Registry, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND workspace_id = ? AND slug = ? AND deleted_at IS NULL", organizationID, workspaceID, slug))
}

func (r *registryRepository) get(q *gorm.DB) (*delivery.Registry, error) {
	var entity registryEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *registryRepository) ListRegistries(ctx context.Context, query delivery.ListRegistriesQuery) (*pagination.Result[*delivery.Registry], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&registryEntity{}).Where("organization_id = ? AND workspace_id = ?", query.OrganizationID, query.WorkspaceID)
		if !query.IncludeArchived {
			q = q.Where("deleted_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, registryKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, r.mapper.ToDomain)
}

func (r *registryRepository) UpdateRegistry(ctx context.Context, reg *delivery.Registry) error {
	e := r.mapper.ToPersistence(reg)
	return updated(r.conn(ctx).Model(&registryEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"slug":           e.Slug,
			"display_name":   e.DisplayName,
			"url":            e.URL,
			"credential_ref": e.CredentialRef,
			"lookup_from":    e.LookupFrom,
			"lookup_pool":    e.LookupPool,
			"updated_at":     e.UpdatedAt,
			"deleted_at":     e.DeletedAt,
		}))
}

func (r *registryRepository) CountActiveRegistries(ctx context.Context, organizationID, workspaceID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&registryEntity{}).
		Where("organization_id = ? AND workspace_id = ? AND deleted_at IS NULL", organizationID, workspaceID).
		Count(&n).Error
	return int(n), translate(err)
}
