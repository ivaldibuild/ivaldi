package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type passwordCredentialRepository struct {
	db     *gorm.DB
	mapper *passwordCredentialMapper
}

// NewPasswordCredentialRepository returns the PostgreSQL
// PasswordCredentialRepository. It uses the transaction carried by the
// context when there is one.
func NewPasswordCredentialRepository(db *gorm.DB) iam.PasswordCredentialRepository {
	return &passwordCredentialRepository{db: db, mapper: newPasswordCredentialMapper()}
}

func (r *passwordCredentialRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *passwordCredentialRepository) Get(ctx context.Context, accountID string) (*iam.PasswordCredential, error) {
	var entity passwordCredentialEntity
	if err := r.conn(ctx).Where("account_id = ?", accountID).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write stores the Account's password, replacing the one it had. An Account
// that does not exist fails with ErrNotFound.
func (r *passwordCredentialRepository) Write(ctx context.Context, cred *iam.PasswordCredential) error {
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"password_hash", "updated_at"}),
	}).Create(r.mapper.ToPersistence(cred)).Error)
}
