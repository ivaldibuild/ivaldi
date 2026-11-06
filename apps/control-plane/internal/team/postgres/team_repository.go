// Package postgres implements team.TeamRepository with GORM on the team
// schema created by ../migrations.
package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type teamRepository struct {
	db     *gorm.DB
	mapper *teamMapper
}

// NewTeamRepository returns the PostgreSQL TeamRepository. It uses the
// transaction carried by the context when there is one.
func NewTeamRepository(db *gorm.DB) team.TeamRepository {
	return &teamRepository{db: db, mapper: newTeamMapper()}
}

func (r *teamRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *teamRepository) CreateTeam(ctx context.Context, t *team.Team) error {
	rows := r.mapper.ToPersistence(t)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rows.team).Error; err != nil {
			return err
		}
		if len(rows.members) > 0 {
			return tx.Create(&rows.members).Error
		}
		return nil
	}))
}

func (r *teamRepository) GetTeam(ctx context.Context, organizationID, teamID string) (*team.Team, error) {
	return r.get(r.conn(ctx), organizationID, false, "id = ?", teamID)
}

func (r *teamRepository) GetTeamForUpdate(ctx context.Context, organizationID, teamID string) (*team.Team, error) {
	return r.get(r.conn(ctx), organizationID, true, "id = ?", teamID)
}

func (r *teamRepository) GetTeamBySlug(ctx context.Context, organizationID, slug string) (*team.Team, error) {
	return r.get(r.conn(ctx), organizationID, false, "slug = ? AND archived_at IS NULL", slug)
}

func (r *teamRepository) get(db *gorm.DB, organizationID string, lock bool, where string, args ...any) (*team.Team, error) {
	q := db.Where("organization_id = ?", organizationID).Where(where, args...)
	if lock {
		q = q.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
	}
	var entity teamEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	ts, err := r.withMembers(db, organizationID, []teamEntity{entity})
	if err != nil {
		return nil, err
	}
	return ts[0], nil
}

// withMembers loads the members of the Teams and builds them.
func (r *teamRepository) withMembers(db *gorm.DB, organizationID string, entities []teamEntity) ([]*team.Team, error) {
	if len(entities) == 0 {
		return []*team.Team{}, nil
	}
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	var members []teamMemberEntity
	err := db.Where("organization_id = ? AND team_id IN ?", organizationID, ids).Order("team_id, principal_id").Find(&members).Error
	if err != nil {
		return nil, translate(err)
	}
	membersOf := map[string][]teamMemberEntity{}
	for _, m := range members {
		membersOf[m.TeamID] = append(membersOf[m.TeamID], m)
	}
	out := make([]*team.Team, 0, len(entities))
	for i := range entities {
		t, err := r.mapper.ToDomain(teamRows{team: &entities[i], members: membersOf[entities[i].ID]})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (r *teamRepository) ListTeams(ctx context.Context, query team.ListTeamsQuery) (*pagination.Result[*team.Team], error) {
	db := r.conn(ctx)
	base := func() *gorm.DB {
		q := db.Model(&teamEntity{}).Where("organization_id = ?", query.OrganizationID)
		if !query.IncludeArchived {
			q = q.Where("archived_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, teamKeyset)
	if err != nil {
		return nil, err
	}
	items, err := r.withMembers(db, query.OrganizationID, p.Rows)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(items, p.Total, p.HasNext, p.HasPrevious, p.Next, p.Previous), nil
}

// UpdateTeam writes the Team's row and replaces its members: kept members
// are updated in place, so the time they joined survives, new ones are
// inserted, and removed ones are deleted.
func (r *teamRepository) UpdateTeam(ctx context.Context, t *team.Team) error {
	rows := r.mapper.ToPersistence(t)
	e := rows.team
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&teamEntity{}).
			Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
			Updates(map[string]any{
				"slug":         e.Slug,
				"display_name": e.DisplayName,
				"description":  e.Description,
				"updated_at":   e.UpdatedAt,
				"archived_at":  e.ArchivedAt,
			})
		if err := updated(res); err != nil {
			return err
		}
		keep := make([]string, len(rows.members))
		for i, m := range rows.members {
			keep[i] = m.PrincipalID
		}
		remove := tx.Where("organization_id = ? AND team_id = ?", e.OrganizationID, e.ID)
		if len(keep) > 0 {
			remove = remove.Where("principal_id NOT IN ?", keep)
		}
		if err := remove.Delete(&teamMemberEntity{}).Error; err != nil {
			return err
		}
		if len(rows.members) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "team_id"}, {Name: "principal_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
		}).Create(&rows.members).Error
	}))
}
