// Package postgres stores the credentials capability's records in PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

type secretStoreRepository struct {
	db *gorm.DB
}

// NewSecretStoreRepository returns the PostgreSQL SecretStoreRepository. It
// uses the transaction carried by the context when there is one.
func NewSecretStoreRepository(db *gorm.DB) credentials.SecretStoreRepository {
	return &secretStoreRepository{db: db}
}

func (r *secretStoreRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// translate maps PostgreSQL errors to the capability's errors.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return credentials.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		if c == "idx_secret_stores_workspace_slug" {
			return fmt.Errorf("%w: %w", credentials.ErrSlugTaken, err)
		}
		return fmt.Errorf("%w: %w", credentials.ErrConflict, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", credentials.ErrInvalid, err)
	}
	return err
}

func toEntity(s *credentials.SecretStore) *secretStoreEntity {
	snap := s.Snapshot()
	return &secretStoreEntity{
		ID:              snap.ID,
		OrganizationID:  snap.OrganizationID,
		WorkspaceID:     snap.WorkspaceID,
		Slug:            snap.Slug,
		DisplayName:     snap.DisplayName,
		Kind:            string(snap.Kind),
		Settings:        snap.Settings,
		Access:          snap.Access,
		EnvironmentIDs:  database.TextArray(snap.EnvironmentIDs),
		CredentialSetAt: snap.CredentialSetAt,
		CredentialSetBy: snap.CredentialSetBy,
		CreatedAt:       snap.CreatedAt,
		UpdatedAt:       snap.UpdatedAt,
		ArchivedAt:      snap.ArchivedAt,
	}
}

func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func toDomain(e *secretStoreEntity) (*credentials.SecretStore, error) {
	props := credentials.NewSecretStoreProps{
		OrganizationID:  e.OrganizationID,
		WorkspaceID:     e.WorkspaceID,
		Slug:            e.Slug,
		DisplayName:     e.DisplayName,
		Kind:            secretstore.Kind(e.Kind),
		Settings:        e.Settings,
		Access:          e.Access,
		EnvironmentIDs:  e.EnvironmentIDs,
		CredentialSetAt: utcPtr(e.CredentialSetAt), CredentialSetBy: e.CredentialSetBy,
		CreatedAt: utc(e.CreatedAt), UpdatedAt: utc(e.UpdatedAt),
	}

	if e.ArchivedAt != nil {
		props.ArchivedAt = utc(*e.ArchivedAt)
	}

	return credentials.NewSecretStore(props, &e.ID)
}

func toDomains(entities []secretStoreEntity) ([]*credentials.SecretStore, error) {
	out := make([]*credentials.SecretStore, 0, len(entities))
	for i := range entities {
		s, err := toDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *secretStoreRepository) CreateSecretStore(ctx context.Context, s *credentials.SecretStore) error {
	return translate(r.conn(ctx).Create(toEntity(s)).Error)
}

func (r *secretStoreRepository) GetSecretStore(ctx context.Context, organizationID, storeID string) (*credentials.SecretStore, error) {
	var e secretStoreEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, storeID).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return toDomain(&e)
}

func (r *secretStoreRepository) GetSecretStoreForUpdate(ctx context.Context, organizationID, storeID string) (*credentials.SecretStore, error) {
	var e secretStoreEntity
	err := r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, storeID).Take(&e).Error
	if err != nil {
		return nil, translate(err)
	}
	return toDomain(&e)
}

func (r *secretStoreRepository) GetSecretStores(ctx context.Context, organizationID string, storeIDs []string) ([]*credentials.SecretStore, error) {
	return r.stores(r.conn(ctx), organizationID, storeIDs)
}

func (r *secretStoreRepository) GetSecretStoresForShare(ctx context.Context, organizationID string, storeIDs []string) ([]*credentials.SecretStore, error) {
	return r.stores(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthShare}), organizationID, storeIDs)
}

// stores reads the stores among the IDs, in ID order.
func (r *secretStoreRepository) stores(q *gorm.DB, organizationID string, storeIDs []string) ([]*credentials.SecretStore, error) {
	if len(storeIDs) == 0 {
		return []*credentials.SecretStore{}, nil
	}
	var entities []secretStoreEntity
	if err := q.Where("organization_id = ? AND id IN ?", organizationID, storeIDs).Order("id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	return toDomains(entities)
}

func (r *secretStoreRepository) ListSecretStores(ctx context.Context, organizationID, workspaceID string, includeArchived bool) ([]*credentials.SecretStore, error) {
	q := r.conn(ctx).Where("organization_id = ? AND workspace_id = ?", organizationID, workspaceID)
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	var entities []secretStoreEntity
	if err := q.Order("lower(display_name), id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	return toDomains(entities)
}

func (r *secretStoreRepository) UpdateSecretStore(ctx context.Context, s *credentials.SecretStore) error {
	e := toEntity(s)
	// A struct update runs the JSON serializer; Select writes the zero
	// values too, such as a cleared archived_at.
	res := r.conn(ctx).Model(&secretStoreEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Select("slug", "display_name", "settings", "access", "environment_ids", "credential_set_at", "credential_set_by", "updated_at", "archived_at").
		Updates(e)
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return credentials.ErrNotFound
	}
	return nil
}

func (r *secretStoreRepository) CountActiveSecretStores(ctx context.Context, organizationID, workspaceID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&secretStoreEntity{}).
		Where("organization_id = ? AND workspace_id = ? AND archived_at IS NULL", organizationID, workspaceID).
		Count(&n).Error
	return int(n), translate(err)
}
