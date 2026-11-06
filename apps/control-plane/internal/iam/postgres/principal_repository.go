package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type principalRepository struct {
	db *gorm.DB
}

// NewPrincipalRepository returns the PostgreSQL PrincipalRepository. It uses
// the transaction carried by the context when there is one.
func NewPrincipalRepository(db *gorm.DB) iam.PrincipalRepository {
	return &principalRepository{db: db}
}

func (r *principalRepository) Get(ctx context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error) {
	var p principalEntity
	err := database.Conn(ctx, r.db).Where("organization_id = ? AND id = ?", organizationID, principalID).Take(&p).Error
	if err != nil {
		return iam.PrincipalSnapshot{}, translate(err)
	}
	return principalSnapshot(&p)
}
