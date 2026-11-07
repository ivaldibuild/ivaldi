package postgres

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
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

// agentRow is an Agent's principal and agents rows together, for listing.
type agentRow struct {
	ID             string    `gorm:"column:id"`
	OrganizationID string    `gorm:"column:organization_id"`
	DisplayName    string    `gorm:"column:display_name"`
	Status         string    `gorm:"column:status"`
	Metadata       *string   `gorm:"column:metadata"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
	Slug           string    `gorm:"column:slug"`
}

var agentKeyset = database.Keyset[agentRow]{
	Keys: map[string]database.SortKey[agentRow]{
		"created_at":   database.TimeKey("created_at", func(r *agentRow) time.Time { return r.CreatedAt }),
		"display_name": database.StringKey("display_name", func(r *agentRow) string { return r.DisplayName }),
		"slug":         database.StringKey("slug", func(r *agentRow) string { return r.Slug }),
	},
	Default: "display_name",
	ID:      func(r *agentRow) string { return r.ID },
}

func (r *agentRepository) List(ctx context.Context, query iam.ListAgentsQuery) (*pagination.Result[*iam.Agent], error) {
	conn := database.Conn(ctx, r.db)
	base := func() *gorm.DB {
		agents := conn.Table("iam.principals p").
			Select("p.id, p.organization_id, p.display_name, p.status, p.metadata, p.created_at, p.updated_at, a.slug").
			Joins("JOIN iam.agents a ON a.organization_id = p.organization_id AND a.principal_id = p.id").
			Where("p.organization_id = ? AND p.kind = ?", query.OrganizationID, string(iam.PrincipalAgent))
		if !query.IncludeInactive {
			agents = agents.Where("p.status = ?", string(iam.PrincipalActive))
		}
		if query.Search != "" {
			pattern := "%" + escapeLike(query.Search) + "%"
			agents = agents.Where(`(p.display_name ILIKE ? ESCAPE '\' OR a.slug ILIKE ? ESCAPE '\')`, pattern, pattern)
		}
		return conn.Table("(?) AS agents", agents)
	}
	page, err := database.Paginate(base, query.Page, agentKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, iam.NewErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, translate(err)
	}
	return database.ToResult(page, func(row *agentRow) (*iam.Agent, error) {
		a, err := r.mapper.ToDomain(&principalEntity{
			ID: row.ID, OrganizationID: row.OrganizationID, Kind: string(iam.PrincipalAgent),
			DisplayName: row.DisplayName, Status: row.Status, Metadata: row.Metadata,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, &agentEntity{PrincipalID: row.ID, OrganizationID: row.OrganizationID, Slug: row.Slug})
		if err != nil {
			return nil, err
		}
		return a, nil
	})
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
