package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type identityProviderRepository struct {
	db     *gorm.DB
	mapper *identityProviderMapper
}

// NewIdentityProviderRepository returns the PostgreSQL
// IdentityProviderRepository. It uses the transaction carried by the context
// when there is one.
func NewIdentityProviderRepository(db *gorm.DB) iam.IdentityProviderRepository {
	return &identityProviderRepository{db: db, mapper: newIdentityProviderMapper()}
}

func (r *identityProviderRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *identityProviderRepository) Get(ctx context.Context, organizationID, identityProviderID string) (*iam.IdentityProvider, error) {
	var entity identityProviderEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, identityProviderID).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *identityProviderRepository) List(ctx context.Context, organizationID string) ([]*iam.IdentityProvider, error) {
	var entities []identityProviderEntity
	if err := r.conn(ctx).Where("organization_id = ?", organizationID).Order("created_at, id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	providers := make([]*iam.IdentityProvider, 0, len(entities))
	for i := range entities {
		p, err := r.mapper.ToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}

// Write inserts the identity provider, or updates it when its ID is stored.
// A provider never moves to another Organization or protocol.
func (r *identityProviderRepository) Write(ctx context.Context, p *iam.IdentityProvider) error {
	e, err := r.mapper.ToPersistence(p)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "config", "secret_ref", "jit_provisioning", "status", "updated_at"}),
	}).Create(e).Error)
}
