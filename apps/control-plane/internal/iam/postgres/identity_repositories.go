package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type externalIdentityRepository struct {
	db     *gorm.DB
	mapper *externalIdentityMapper
}

// NewExternalIdentityRepository returns the PostgreSQL
// ExternalIdentityRepository. It uses the transaction carried by the context
// when there is one.
func NewExternalIdentityRepository(db *gorm.DB) iam.ExternalIdentityRepository {
	return &externalIdentityRepository{db: db, mapper: newExternalIdentityMapper()}
}

func (r *externalIdentityRepository) GetBySubject(ctx context.Context, organizationID, identityProviderID, subject string) (*iam.ExternalIdentity, error) {
	var entity externalIdentityEntity
	err := database.Conn(ctx, r.db).
		Where("organization_id = ? AND identity_provider_id = ? AND subject = ?", organizationID, identityProviderID, subject).
		Take(&entity).Error
	if err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write inserts the link, or records its last sign-in when it is stored. A
// subject already linked to another User fails with ErrAlreadyExists.
func (r *externalIdentityRepository) Write(ctx context.Context, x *iam.ExternalIdentity) error {
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_login_at"}),
	}).Create(r.mapper.ToPersistence(x)).Error)
}

type accountIdentityRepository struct {
	db     *gorm.DB
	mapper *accountIdentityMapper
}

// NewAccountIdentityRepository returns the PostgreSQL
// AccountIdentityRepository. It uses the transaction carried by the context
// when there is one.
func NewAccountIdentityRepository(db *gorm.DB) iam.AccountIdentityRepository {
	return &accountIdentityRepository{db: db, mapper: newAccountIdentityMapper()}
}

func (r *accountIdentityRepository) GetBySubject(ctx context.Context, provider iam.PlatformProvider, subject string) (*iam.AccountIdentity, error) {
	var entity accountIdentityEntity
	err := database.Conn(ctx, r.db).Where("provider = ? AND subject = ?", string(provider), subject).Take(&entity).Error
	if err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write inserts the link, or records its last sign-in when it is stored. A
// subject already linked to another Account fails with ErrAlreadyExists.
func (r *accountIdentityRepository) Write(ctx context.Context, a *iam.AccountIdentity) error {
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_login_at"}),
	}).Create(r.mapper.ToPersistence(a)).Error)
}
