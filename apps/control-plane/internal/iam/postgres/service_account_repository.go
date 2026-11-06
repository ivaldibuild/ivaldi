package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type serviceAccountEntity struct {
	PrincipalID          string `gorm:"column:principal_id;primaryKey"`
	OrganizationID       string `gorm:"column:organization_id"`
	Description          string `gorm:"column:description"`
	CreatedByPrincipalID string `gorm:"column:created_by_principal_id"`
}

func (serviceAccountEntity) TableName() string { return "iam.service_accounts" }

type apiTokenEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	PrincipalID    string     `gorm:"column:principal_id"`
	Name           string     `gorm:"column:name"`
	TokenPrefix    string     `gorm:"column:token_prefix"`
	TokenHash      []byte     `gorm:"column:token_hash"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
	LastUsedAt     *time.Time `gorm:"column:last_used_at"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (apiTokenEntity) TableName() string { return "iam.api_tokens" }

type serviceAccountRepository struct {
	db *gorm.DB
}

// NewServiceAccountRepository returns the PostgreSQL ServiceAccountRepository.
// It uses the transaction carried by the context when there is one.
func NewServiceAccountRepository(db *gorm.DB) iam.ServiceAccountRepository {
	return &serviceAccountRepository{db: db}
}

func (r *serviceAccountRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// Write inserts the service account's principal and service_accounts rows
// together, or updates the principal when its ID is stored.
func (r *serviceAccountRepository) Write(ctx context.Context, s *iam.ServiceAccount) error {
	snap := s.Snapshot()
	metadata, err := encodeMetadata(snap.Metadata)
	if err != nil {
		return err
	}
	p := &principalEntity{
		ID: snap.ID, OrganizationID: snap.OrganizationID, Kind: string(snap.Kind), DisplayName: snap.DisplayName,
		Status: string(snap.Status), Metadata: metadata, CreatedAt: snap.CreatedAt, UpdatedAt: snap.UpdatedAt,
	}
	sa := &serviceAccountEntity{
		PrincipalID: snap.ID, OrganizationID: snap.OrganizationID, Description: snap.Description,
		CreatedByPrincipalID: snap.CreatedByPrincipalID,
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
			DoUpdates: clause.AssignmentColumns([]string{"description"}),
		}).Create(sa).Error
	}))
}

func (r *serviceAccountRepository) Get(ctx context.Context, organizationID, principalID string) (*iam.ServiceAccount, error) {
	out, err := r.list(ctx, r.conn(ctx).Where("p.organization_id = ? AND p.id = ?", organizationID, principalID))
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, iam.ErrNotFound
	}
	return out[0], nil
}

func (r *serviceAccountRepository) List(ctx context.Context, organizationID string, includeInactive bool) ([]*iam.ServiceAccount, error) {
	q := r.conn(ctx).Where("p.organization_id = ?", organizationID)
	if !includeInactive {
		q = q.Where("p.status = ?", string(iam.PrincipalActive))
	}
	return r.list(ctx, q.Order("p.display_name, p.id"))
}

// serviceAccountRow is a service account's principal joined with its row.
// It is flat: GORM does not fill an embedded entity from a joined scan.
type serviceAccountRow struct {
	ID                   string    `gorm:"column:id"`
	OrganizationID       string    `gorm:"column:organization_id"`
	DisplayName          string    `gorm:"column:display_name"`
	Status               string    `gorm:"column:status"`
	Metadata             *string   `gorm:"column:metadata"`
	CreatedAt            time.Time `gorm:"column:created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
	Description          string    `gorm:"column:description"`
	CreatedByPrincipalID string    `gorm:"column:created_by_principal_id"`
}

func (r *serviceAccountRepository) list(_ context.Context, q *gorm.DB) ([]*iam.ServiceAccount, error) {
	var rows []serviceAccountRow
	err := q.Table("iam.principals p").
		Select("p.id, p.organization_id, p.display_name, p.status, p.metadata, p.created_at, p.updated_at, s.description, s.created_by_principal_id").
		Joins("JOIN iam.service_accounts s ON s.organization_id = p.organization_id AND s.principal_id = p.id").
		Scan(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.ServiceAccount, len(rows))
	for i, row := range rows {
		metadata, err := decodeMetadata(row.Metadata)
		if err != nil {
			return nil, err
		}
		s, err := iam.NewServiceAccount(iam.NewServiceAccountProps{
			PrincipalProps: iam.PrincipalProps{
				OrganizationID: row.OrganizationID, DisplayName: row.DisplayName, Status: iam.PrincipalStatus(row.Status),
				Metadata: metadata, CreatedAt: utc(row.CreatedAt), UpdatedAt: utc(row.UpdatedAt),
			},
			Description:          row.Description,
			CreatedByPrincipalID: row.CreatedByPrincipalID,
		}, &row.ID)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

type apiTokenRepository struct {
	db *gorm.DB
}

// NewAPITokenRepository returns the PostgreSQL APITokenRepository. It uses
// the transaction carried by the context when there is one.
func NewAPITokenRepository(db *gorm.DB) iam.APITokenRepository {
	return &apiTokenRepository{db: db}
}

func (r *apiTokenRepository) conn(ctx context.Context) *gorm.DB { return database.Conn(ctx, r.db) }

func (r *apiTokenRepository) Write(ctx context.Context, t *iam.APIToken) error {
	s := t.Snapshot()
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_used_at", "revoked_at"}),
	}).Create(&apiTokenEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, PrincipalID: s.PrincipalID, Name: s.Name,
		TokenPrefix: s.TokenPrefix, TokenHash: s.TokenHash, ExpiresAt: s.ExpiresAt,
		LastUsedAt: s.LastUsedAt, RevokedAt: s.RevokedAt, CreatedAt: s.CreatedAt,
	}).Error)
}

func (r *apiTokenRepository) Get(ctx context.Context, organizationID, tokenID string) (*iam.APIToken, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, tokenID))
}

func (r *apiTokenRepository) GetByHash(ctx context.Context, hash []byte) (*iam.APIToken, error) {
	return r.get(r.conn(ctx).Where("token_hash = ?", hash))
}

func (r *apiTokenRepository) get(q *gorm.DB) (*iam.APIToken, error) {
	var e apiTokenEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return apiTokenToDomain(&e)
}

func (r *apiTokenRepository) ListByPrincipal(ctx context.Context, organizationID, principalID string) ([]*iam.APIToken, error) {
	var entities []apiTokenEntity
	err := r.conn(ctx).Where("organization_id = ? AND principal_id = ?", organizationID, principalID).
		Order("created_at DESC, id DESC").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.APIToken, len(entities))
	for i := range entities {
		t, err := apiTokenToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

func apiTokenToDomain(e *apiTokenEntity) (*iam.APIToken, error) {
	return iam.NewAPIToken(iam.NewAPITokenProps{
		OrganizationID: e.OrganizationID, PrincipalID: e.PrincipalID, Name: e.Name, TokenPrefix: e.TokenPrefix,
		TokenHash: e.TokenHash, ExpiresAt: utcPtr(e.ExpiresAt), LastUsedAt: utcPtr(e.LastUsedAt),
		RevokedAt: utcPtr(e.RevokedAt), CreatedAt: utc(e.CreatedAt),
	}, &e.ID)
}
