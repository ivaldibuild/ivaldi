package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type agentRepository struct {
	db     *gorm.DB
	mapper *agentMapper
}

// NewAgentRepository returns the PostgreSQL AgentRepository. It uses the
// transaction carried by the context when there is one.
func NewAgentRepository(db *gorm.DB) iam.AgentRepository {
	return &agentRepository{db: db, mapper: newAgentMapper()}
}

func (r *agentRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *agentRepository) Get(ctx context.Context, organizationID, principalID string) (*iam.Agent, error) {
	return r.get(ctx, r.conn(ctx).Where("organization_id = ? AND principal_id = ?", organizationID, principalID))
}

func (r *agentRepository) GetBySlug(ctx context.Context, organizationID, slug string) (*iam.Agent, error) {
	return r.get(ctx, r.conn(ctx).Where("organization_id = ? AND slug = ?", organizationID, slug))
}

func (r *agentRepository) get(ctx context.Context, q *gorm.DB) (*iam.Agent, error) {
	var a agentEntity
	if err := q.Take(&a).Error; err != nil {
		return nil, translate(err)
	}
	var p principalEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", a.OrganizationID, a.PrincipalID).Take(&p).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&p, &a)
}

// Write inserts the Agent's principal and agent rows together, or updates
// the principal when its ID is stored. An Agent's slug never changes. A slug
// another of the Organization's Agents holds fails with ErrAlreadyExists,
// and a missing Organization with ErrNotFound.
func (r *agentRepository) Write(ctx context.Context, agent *iam.Agent) error {
	p, a, err := r.mapper.ToPersistence(agent)
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
			DoNothing: true,
		}).Create(a).Error
	}))
}
