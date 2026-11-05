package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// forUpdate locks the rows a query reads until the transaction ends.
var forUpdate = clause.Locking{Strength: clause.LockingStrengthUpdate}

type oauthClientRepository struct {
	db     *gorm.DB
	mapper *oauthClientMapper
}

// NewOAuthClientRepository returns the PostgreSQL OAuthClientRepository. It
// uses the transaction carried by the context when there is one.
func NewOAuthClientRepository(db *gorm.DB) iam.OAuthClientRepository {
	return &oauthClientRepository{db: db, mapper: newOAuthClientMapper()}
}

func (r *oauthClientRepository) Get(ctx context.Context, id string) (*iam.OAuthClient, error) {
	return r.get(database.Conn(ctx, r.db).Where("id = ?", id))
}

func (r *oauthClientRepository) GetByClientID(ctx context.Context, clientID string) (*iam.OAuthClient, error) {
	return r.get(database.Conn(ctx, r.db).Where("client_id = ?", clientID))
}

func (r *oauthClientRepository) get(q *gorm.DB) (*iam.OAuthClient, error) {
	var entity oauthClientEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write inserts the client, or updates it when its ID is stored. A client_id
// another client holds fails with ErrAlreadyExists.
func (r *oauthClientRepository) Write(ctx context.Context, c *iam.OAuthClient) error {
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "redirect_uris", "last_used_at"}),
	}).Create(r.mapper.ToPersistence(c)).Error)
}

type authorizationCodeRepository struct {
	db     *gorm.DB
	mapper *authorizationCodeMapper
}

// NewOAuthAuthorizationCodeRepository returns the PostgreSQL
// OAuthAuthorizationCodeRepository. It uses the transaction carried by the
// context when there is one.
func NewOAuthAuthorizationCodeRepository(db *gorm.DB) iam.OAuthAuthorizationCodeRepository {
	return &authorizationCodeRepository{db: db, mapper: newAuthorizationCodeMapper()}
}

func (r *authorizationCodeRepository) GetForUpdate(ctx context.Context, codeHash []byte) (*iam.OAuthAuthorizationCode, error) {
	var entity authorizationCodeEntity
	if err := database.Conn(ctx, r.db).Clauses(forUpdate).Where("code_hash = ?", codeHash).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

// Write inserts the code, or records its use when it is stored.
func (r *authorizationCodeRepository) Write(ctx context.Context, c *iam.OAuthAuthorizationCode) error {
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"used_at"}),
	}).Create(r.mapper.ToPersistence(c)).Error)
}

type refreshTokenRepository struct {
	db     *gorm.DB
	mapper *refreshTokenMapper
}

// NewOAuthRefreshTokenRepository returns the PostgreSQL
// OAuthRefreshTokenRepository. It uses the transaction carried by the
// context when there is one.
func NewOAuthRefreshTokenRepository(db *gorm.DB) iam.OAuthRefreshTokenRepository {
	return &refreshTokenRepository{db: db, mapper: newRefreshTokenMapper()}
}

func (r *refreshTokenRepository) GetByHashForUpdate(ctx context.Context, tokenHash []byte) (*iam.OAuthRefreshToken, error) {
	var entity refreshTokenEntity
	if err := database.Conn(ctx, r.db).Clauses(forUpdate).Where("token_hash = ?", tokenHash).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *refreshTokenRepository) ListActiveByFamily(ctx context.Context, organizationID, familyID string) ([]*iam.OAuthRefreshToken, error) {
	var entities []refreshTokenEntity
	err := database.Conn(ctx, r.db).Clauses(forUpdate).
		Where("organization_id = ? AND family_id = ? AND revoked_at IS NULL", organizationID, familyID).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	tokens := make([]*iam.OAuthRefreshToken, 0, len(entities))
	for i := range entities {
		t, err := r.mapper.ToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// Write inserts the token, or records its rotation or revocation when it is
// stored. A rotated token refers to its replacement, which must be written
// first, or the write fails with ErrNotFound.
func (r *refreshTokenRepository) Write(ctx context.Context, t *iam.OAuthRefreshToken) error {
	return translate(database.Conn(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"replaced_by", "revoked_at"}),
	}).Create(r.mapper.ToPersistence(t)).Error)
}
