package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type secretStoreCredentialRepository struct {
	db *gorm.DB
}

// NewSecretStoreCredentialRepository returns the PostgreSQL
// SecretStoreCredentialRepository and DataKeyRepository. It uses the
// transaction carried by the context when there is one.
func NewSecretStoreCredentialRepository(db *gorm.DB) interface {
	credentials.SecretStoreCredentialRepository
	credentials.DataKeyRepository
} {
	return &secretStoreCredentialRepository{db: db}
}

func (r *secretStoreCredentialRepository) PutSealedCredential(ctx context.Context, c credentials.SealedCredential) error {
	e := secretStoreCredentialEntity{StoreID: c.StoreID, OrganizationID: c.OrganizationID, Ciphertext: c.Ciphertext, UpdatedAt: clock.Now()}
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "secret_store_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ciphertext", "updated_at"}),
	}).Create(&e).Error)
}

func (r *secretStoreCredentialRepository) GetSealedCredential(ctx context.Context, organizationID, storeID string) (credentials.SealedCredential, error) {
	var e secretStoreCredentialEntity
	err := database.Conn(ctx, r.db).Where("organization_id = ? AND secret_store_id = ?", organizationID, storeID).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return credentials.SealedCredential{}, credentials.ErrNotFound
	}
	if err != nil {
		return credentials.SealedCredential{}, translate(err)
	}
	return credentials.SealedCredential{OrganizationID: e.OrganizationID, StoreID: e.StoreID, Ciphertext: e.Ciphertext}, nil
}

func (r *secretStoreCredentialRepository) DeleteSealedCredential(ctx context.Context, organizationID, storeID string) error {
	return translate(database.Conn(ctx, r.db).
		Where("organization_id = ? AND secret_store_id = ?", organizationID, storeID).
		Delete(&secretStoreCredentialEntity{}).Error)
}

func (r *secretStoreCredentialRepository) GetDataKey(ctx context.Context, organizationID string) (credentials.DataKey, error) {
	var e dataKeyEntity
	err := database.Conn(ctx, r.db).Where("organization_id = ?", organizationID).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return credentials.DataKey{}, credentials.ErrNotFound
	}
	if err != nil {
		return credentials.DataKey{}, translate(err)
	}
	return credentials.DataKey{OrganizationID: e.OrganizationID, KeyID: e.KeyID, Wrapped: e.WrappedKey}, nil
}

func (r *secretStoreCredentialRepository) CreateDataKeyIfAbsent(ctx context.Context, k credentials.DataKey) error {
	e := dataKeyEntity{OrganizationID: k.OrganizationID, KeyID: k.KeyID, WrappedKey: k.Wrapped, CreatedAt: clock.Now()}
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&e).Error)
}
