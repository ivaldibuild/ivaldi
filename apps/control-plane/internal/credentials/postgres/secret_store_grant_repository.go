package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type secretStoreGrantRepository struct {
	db *gorm.DB
}

// NewSecretStoreGrantRepository returns the PostgreSQL
// SecretStoreGrantRepository. It uses the transaction carried by the
// context when there is one.
func NewSecretStoreGrantRepository(db *gorm.DB) credentials.SecretStoreGrantRepository {
	return &secretStoreGrantRepository{db: db}
}

func (r *secretStoreGrantRepository) CreateSecretStoreGrants(ctx context.Context, grants []*credentials.SecretStoreGrant) error {
	if len(grants) == 0 {
		return nil
	}
	entities := make([]secretStoreGrantEntity, len(grants))
	for i, g := range grants {
		snap := g.Snapshot()
		entities[i] = secretStoreGrantEntity{
			ID: snap.ID, OrganizationID: snap.OrganizationID, StoreID: snap.StoreID, DeploymentID: snap.DeploymentID,
			Mode: string(snap.Mode), TokenID: snap.TokenID, ExpiresAt: snap.ExpiresAt, RevokedAt: snap.RevokedAt,
			CreatedAt: snap.CreatedAt,
		}
	}
	return translate(database.Conn(ctx, r.db).Create(&entities).Error)
}

func (r *secretStoreGrantRepository) ListDeploymentGrants(ctx context.Context, organizationID, deploymentID string) ([]*credentials.SecretStoreGrant, error) {
	var entities []secretStoreGrantEntity
	err := database.Conn(ctx, r.db).
		Where("organization_id = ? AND deployment_id = ?", organizationID, deploymentID).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*credentials.SecretStoreGrant, 0, len(entities))
	for _, e := range entities {
		g, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: e.OrganizationID, StoreID: e.StoreID, DeploymentID: e.DeploymentID,
			Mode: credentials.AccessMode(e.Mode), TokenID: e.TokenID, ExpiresAt: e.ExpiresAt,
			RevokedAt: utcPtr(e.RevokedAt), CreatedAt: utc(e.CreatedAt),
		}, &e.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return utc(*t)
}
