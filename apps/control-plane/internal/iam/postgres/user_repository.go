package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type userRepository struct {
	db     *gorm.DB
	mapper *userMapper
}

// NewUserRepository returns the PostgreSQL UserRepository. It uses the
// transaction carried by the context when there is one.
func NewUserRepository(db *gorm.DB) iam.UserRepository {
	return &userRepository{db: db, mapper: newUserMapper()}
}

func (r *userRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// Write inserts the User's principal and membership rows together, or
// updates the principal when its ID is stored. A membership never changes
// Organization or Account. A second membership of an Account in an
// Organization fails with ErrAlreadyExists, and a missing Organization or
// Account with ErrNotFound.
func (r *userRepository) Write(ctx context.Context, user *iam.User) error {
	p, u, err := r.mapper.ToPersistence(user)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"display_name", "status", "metadata", "updated_at"}),
		}).Create(p).Error
		if err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "principal_id"}},
			DoNothing: true,
		}).Create(u).Error
	}))
}
