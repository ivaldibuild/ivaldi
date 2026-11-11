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

// NewPrincipalReader returns the PostgreSQL PrincipalReader, which names a
// batch of principals. It reads the same rows as NewPrincipalRepository.
func NewPrincipalReader(db *gorm.DB) iam.PrincipalReader {
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

func (r *principalRepository) ListByIDs(ctx context.Context, organizationID string, principalIDs []string) ([]iam.PrincipalSnapshot, error) {
	if len(principalIDs) == 0 {
		return nil, nil
	}
	// Metadata is left unread: naming a principal does not need it, and one
	// row whose metadata will not decode must not fail the whole batch.
	var entities []principalEntity
	err := database.Conn(ctx, r.db).
		Omit("metadata").
		Where("organization_id = ? AND id IN ?", organizationID, principalIDs).
		Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]iam.PrincipalSnapshot, len(entities))
	for i := range entities {
		out[i], err = principalSnapshot(&entities[i])
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
