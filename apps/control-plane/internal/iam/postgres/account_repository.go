package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type accountRepository struct {
	db     *gorm.DB
	mapper *accountMapper
}

// NewAccountRepository returns the PostgreSQL AccountRepository. It uses the
// transaction carried by the context when there is one.
func NewAccountRepository(db *gorm.DB) iam.AccountRepository {
	return &accountRepository{db: db, mapper: newAccountMapper()}
}

func (r *accountRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// ExistsByEmail ignores case, as the unique index on accounts does.
func (r *accountRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.conn(ctx).Raw("SELECT EXISTS (SELECT 1 FROM iam.accounts WHERE lower(email) = lower(?))", email).Scan(&exists).Error
	return exists, translate(err)
}

// Write inserts the Account, or updates it when its ID is stored. An email
// another Account holds, in any case, fails with ErrAlreadyExists.
func (r *accountRepository) Write(ctx context.Context, account *iam.Account) error {
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"email", "email_verified_at", "display_name", "status", "updated_at"}),
	}).Create(r.mapper.ToPersistence(account)).Error)
}
