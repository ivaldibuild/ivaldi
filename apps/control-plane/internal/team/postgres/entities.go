package postgres

import "time"

// Rows of the team schema. They carry GORM's mapping so the domain types do
// not. IDs and timestamps are set by the domain, never by GORM.

type teamEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	Slug           string     `gorm:"column:slug"`
	DisplayName    string     `gorm:"column:display_name"`
	Description    *string    `gorm:"column:description"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt     *time.Time `gorm:"column:archived_at"`
}

func (teamEntity) TableName() string { return "team.teams" }

type teamMemberEntity struct {
	OrganizationID string    `gorm:"column:organization_id"`
	TeamID         string    `gorm:"column:team_id;primaryKey"`
	PrincipalID    string    `gorm:"column:principal_id;primaryKey"`
	PrincipalKind  string    `gorm:"column:principal_kind"`
	Role           string    `gorm:"column:role"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (teamMemberEntity) TableName() string { return "team.team_members" }

// utc returns t in UTC. The driver returns times in the local zone, and the
// domain records UTC.
func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return utc(*t)
}
