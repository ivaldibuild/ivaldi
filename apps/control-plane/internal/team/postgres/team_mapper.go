package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
)

type teamMapper struct{}

func newTeamMapper() *teamMapper {
	return &teamMapper{}
}

// teamRows are the rows of one Team.
type teamRows struct {
	team    *teamEntity
	members []teamMemberEntity
}

func (m *teamMapper) ToPersistence(t *team.Team) teamRows {
	s := t.Snapshot()
	rows := teamRows{team: &teamEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Description:    s.Description,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}}
	for _, mem := range s.Members {
		rows.members = append(rows.members, teamMemberEntity{
			OrganizationID: s.OrganizationID,
			TeamID:         s.ID,
			PrincipalID:    mem.PrincipalID,
			Role:           string(mem.Role),
			CreatedAt:      mem.CreatedAt,
			UpdatedAt:      mem.UpdatedAt,
		})
	}
	return rows
}

// ToDomain builds a Team from its row and its members' rows.
func (m *teamMapper) ToDomain(rows teamRows) (*team.Team, error) {
	members := make([]team.NewTeamMemberProps, len(rows.members))
	for i, mem := range rows.members {
		members[i] = team.NewTeamMemberProps{
			PrincipalID: mem.PrincipalID,
			Role:        team.Role(mem.Role),
			CreatedAt:   utc(mem.CreatedAt),
			UpdatedAt:   utc(mem.UpdatedAt),
		}
	}
	e := rows.team
	return team.NewTeam(team.NewTeamProps{
		OrganizationID: e.OrganizationID,
		Slug:           e.Slug,
		DisplayName:    e.DisplayName,
		Description:    e.Description,
		Members:        members,
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
		ArchivedAt:     utcPtr(e.ArchivedAt),
	}, &e.ID)
}
