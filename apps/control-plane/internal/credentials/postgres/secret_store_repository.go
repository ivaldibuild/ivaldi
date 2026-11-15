// Package postgres stores the credentials capability's records in PostgreSQL.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// secretStoreEntity is a row of credentials.secret_stores. IDs and
// timestamps are set by the domain, never by GORM.
type secretStoreEntity struct {
	ID             string             `gorm:"column:id;primaryKey"`
	OrganizationID string             `gorm:"column:organization_id"`
	WorkspaceID    string             `gorm:"column:workspace_id"`
	Slug           string             `gorm:"column:slug"`
	DisplayName    string             `gorm:"column:display_name"`
	Kind           string             `gorm:"column:kind"`
	Settings       []byte             `gorm:"column:settings;type:jsonb"`
	EnvironmentIDs database.TextArray `gorm:"column:environment_ids;type:text[]"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt     *time.Time         `gorm:"column:archived_at"`
}

func (secretStoreEntity) TableName() string { return "credentials.secret_stores" }

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

func toEntity(s *credentials.SecretStore) (*secretStoreEntity, error) {
	snap := s.Snapshot()
	settings, err := json.Marshal(snap.Settings)
	if err != nil {
		return nil, fmt.Errorf("encode settings: %w", err)
	}
	return &secretStoreEntity{
		ID: snap.ID, OrganizationID: snap.OrganizationID, WorkspaceID: snap.WorkspaceID,
		Slug: snap.Slug, DisplayName: snap.DisplayName, Kind: string(snap.Kind), Settings: settings,
		EnvironmentIDs: database.TextArray(snap.EnvironmentIDs),
		CreatedAt:      snap.CreatedAt, UpdatedAt: snap.UpdatedAt, ArchivedAt: snap.ArchivedAt,
	}, nil
}

func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func toDomain(e *secretStoreEntity) (*credentials.SecretStore, error) {
	var settings credentials.Settings
	if err := json.Unmarshal(e.Settings, &settings); err != nil {
		return nil, fmt.Errorf("decode the settings of secret store %s: %w", e.ID, err)
	}
	props := credentials.NewSecretStoreProps{
		OrganizationID: e.OrganizationID, WorkspaceID: e.WorkspaceID, Slug: e.Slug, DisplayName: e.DisplayName,
		Kind: secretstore.Kind(e.Kind), Settings: settings, EnvironmentIDs: e.EnvironmentIDs,
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
	e, err := toEntity(s)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *secretStoreRepository) GetSecretStore(ctx context.Context, organizationID, storeID string) (*credentials.SecretStore, error) {
	var e secretStoreEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, storeID).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return toDomain(&e)
}

func (r *secretStoreRepository) GetSecretStores(ctx context.Context, organizationID string, storeIDs []string) ([]*credentials.SecretStore, error) {
	if len(storeIDs) == 0 {
		return []*credentials.SecretStore{}, nil
	}
	var entities []secretStoreEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id IN ?", organizationID, storeIDs).Order("id").Find(&entities).Error; err != nil {
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
	e, err := toEntity(s)
	if err != nil {
		return err
	}
	res := r.conn(ctx).Model(&secretStoreEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"slug": e.Slug, "display_name": e.DisplayName, "settings": e.Settings,
			"environment_ids": e.EnvironmentIDs, "updated_at": e.UpdatedAt, "archived_at": e.ArchivedAt,
		})
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
