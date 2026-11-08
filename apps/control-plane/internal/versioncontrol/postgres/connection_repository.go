// Package postgres implements the version-control repositories with GORM on
// the version_control schema created by ../migrations.
package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type connectionRepository struct {
	db     *gorm.DB
	mapper *connectionMapper
}

// NewConnectionRepository returns the PostgreSQL ConnectionRepository. It
// uses the transaction carried by the context when there is one.
func NewConnectionRepository(db *gorm.DB) versioncontrol.ConnectionRepository {
	return &connectionRepository{db: db, mapper: newConnectionMapper()}
}

func (r *connectionRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *connectionRepository) CreateConnection(ctx context.Context, c *versioncontrol.Connection) error {
	return translate(r.conn(ctx).Create(r.mapper.ToPersistence(c)).Error)
}

func (r *connectionRepository) GetConnection(ctx context.Context, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, connectionID))
}

func (r *connectionRepository) GetConnectionForUpdate(ctx context.Context, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, connectionID))
}

func (r *connectionRepository) get(q *gorm.DB) (*versioncontrol.Connection, error) {
	var entity connectionEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *connectionRepository) ListConnections(ctx context.Context, organizationID string, includeArchived bool) ([]*versioncontrol.Connection, error) {
	q := r.conn(ctx).Model(&connectionEntity{}).Where("organization_id = ?", organizationID)
	if !includeArchived {
		q = q.Where("deleted_at IS NULL")
	}
	var entities []connectionEntity
	if err := q.Order("created_at, id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*versioncontrol.Connection, 0, len(entities))
	for i := range entities {
		c, err := r.mapper.ToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *connectionRepository) UpdateConnection(ctx context.Context, c *versioncontrol.Connection) error {
	e := r.mapper.ToPersistence(c)
	return updated(r.conn(ctx).Model(&connectionEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"provider":              e.Provider,
			"host_url":              e.HostURL,
			"auth_kind":             e.AuthKind,
			"credential_ref":        e.CredentialRef,
			"external_account_id":   e.ExternalAccountID,
			"external_account_name": e.ExternalAccountName,
			"lookup_from":           e.LookupFrom,
			"lookup_pool":           e.LookupPool,
			"status":                e.Status,
			"updated_at":            e.UpdatedAt,
			"deleted_at":            e.DeletedAt,
		}))
}
