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

func (r *userRepository) Get(ctx context.Context, organizationID, principalID string) (*iam.User, error) {
	return r.get(ctx, r.conn(ctx).Where("organization_id = ? AND principal_id = ?", organizationID, principalID))
}

func (r *userRepository) GetByAccount(ctx context.Context, organizationID, accountID string) (*iam.User, error) {
	return r.get(ctx, r.conn(ctx).Where("organization_id = ? AND account_id = ?", organizationID, accountID))
}

func (r *userRepository) get(ctx context.Context, q *gorm.DB) (*iam.User, error) {
	var u userEntity
	if err := q.Take(&u).Error; err != nil {
		return nil, translate(err)
	}
	var p principalEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", u.OrganizationID, u.PrincipalID).Take(&p).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&p, &u)
}

// membershipRow is a User joined with its principal and Organization.
type membershipRow struct {
	OrganizationID     string
	OrganizationSlug   string
	OrganizationName   string
	OrganizationStatus string
	PrincipalID        string
	Status             string
}

func (r *userRepository) ListMemberships(ctx context.Context, accountID string) ([]iam.Membership, error) {
	var rows []membershipRow
	err := r.conn(ctx).Raw(`
		SELECT u.organization_id, o.slug AS organization_slug, o.display_name AS organization_name,
		       o.status AS organization_status, u.principal_id, p.status
		FROM iam.users u
		JOIN iam.principals p ON p.organization_id = u.organization_id AND p.id = u.principal_id
		JOIN iam.organizations o ON o.id = u.organization_id
		WHERE u.account_id = ?
		ORDER BY u.joined_at, u.principal_id`, accountID).Scan(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	memberships := make([]iam.Membership, len(rows))
	for i, row := range rows {
		memberships[i] = iam.Membership{
			OrganizationID:     row.OrganizationID,
			OrganizationSlug:   row.OrganizationSlug,
			OrganizationName:   row.OrganizationName,
			OrganizationStatus: iam.OrganizationStatus(row.OrganizationStatus),
			PrincipalID:        row.PrincipalID,
			Status:             iam.PrincipalStatus(row.Status),
		}
	}
	return memberships, nil
}

// Write inserts the User's principal and membership rows together, or
// updates the principal and the last sign-in time when its ID is stored. A
// membership never changes Organization or Account. A second membership of an Account in an
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
			DoUpdates: clause.AssignmentColumns([]string{"last_signed_in_at"}),
		}).Create(u).Error
	}))
}
