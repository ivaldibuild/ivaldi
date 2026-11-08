package postgres

import "time"

// Rows of the provenance schema. They carry GORM's mapping so the domain
// types do not. IDs and timestamps are set by the domain, never by GORM.

type changeEntity struct {
	ID                   string    `gorm:"column:id;primaryKey"`
	OrganizationID       string    `gorm:"column:organization_id"`
	SourceID             string    `gorm:"column:source_id"`
	SessionID            *string   `gorm:"column:session_id"`
	WorkItemID           *string   `gorm:"column:work_item_id"`
	Title                string    `gorm:"column:title"`
	ExternalRef          *string   `gorm:"column:external_ref"`
	CreatedByPrincipalID *string   `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (changeEntity) TableName() string { return "provenance.changes" }

type changeCommitEntity struct {
	OrganizationID string `gorm:"column:organization_id;primaryKey"`
	SourceID       string `gorm:"column:source_id;primaryKey"`
	CommitSHA      string `gorm:"column:commit_sha;primaryKey"`
	ChangeID       string `gorm:"column:change_id"`
}

func (changeCommitEntity) TableName() string { return "provenance.change_commits" }

func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}
