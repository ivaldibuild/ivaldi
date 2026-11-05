package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type organizationRepository struct {
	db     *gorm.DB
	mapper *organizationMapper
}

// NewOrganizationRepository returns the PostgreSQL OrganizationRepository.
// It uses the transaction carried by the context when there is one.
func NewOrganizationRepository(db *gorm.DB) iam.OrganizationRepository {
	return &organizationRepository{db: db, mapper: newOrganizationMapper()}
}

func (r *organizationRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *organizationRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.conn(ctx).Raw("SELECT EXISTS (SELECT 1 FROM iam.organizations WHERE lower(name) = lower(?))", name).Scan(&exists).Error
	return exists, translate(err)
}

func (r *organizationRepository) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.conn(ctx).Raw("SELECT EXISTS (SELECT 1 FROM iam.organizations WHERE slug = ?)", slug).Scan(&exists).Error
	return exists, translate(err)
}

// GetByIdentifier matches the identifier against IDs and slugs. They cannot
// collide: slugs have no underscore, and IDs always do.
func (r *organizationRepository) GetByIdentifier(ctx context.Context, identifier string) (*iam.Organization, error) {
	var entity organizationEntity
	if err := r.conn(ctx).Where("id = ? OR slug = ?", identifier, identifier).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// GetByIdentifiers returns the Organizations matching any of the
// identifiers, oldest first, leaving out identifiers that match none.
func (r *organizationRepository) GetByIdentifiers(ctx context.Context, identifiers []string) ([]*iam.Organization, error) {
	if len(identifiers) == 0 {
		return nil, nil
	}

	var entities []organizationEntity
	err := r.conn(ctx).
		Where("id IN ? OR slug IN ?", identifiers, identifiers).
		Order("created_at, id").
		Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	orgs := make([]*iam.Organization, 0, len(entities))
	for i := range entities {
		o, err := r.mapper.ToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, nil
}

func (r *organizationRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&organizationEntity{}).Count(&n).Error
	return n, translate(err)
}

func (r *organizationRepository) GetForUpdate(ctx context.Context, organizationID string) (*iam.Organization, error) {
	var entity organizationEntity
	err := r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("id = ?", organizationID).Take(&entity).Error
	if err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write inserts the Organization, or updates it when its ID is stored. A
// slug another Organization holds fails with ErrAlreadyExists.
func (r *organizationRepository) Write(ctx context.Context, org *iam.Organization) error {
	e, err := r.mapper.ToPersistence(org)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"slug", "name", "display_name", "logo_url", "status", "metadata", "updated_at"}),
	}).Create(e).Error)
}
