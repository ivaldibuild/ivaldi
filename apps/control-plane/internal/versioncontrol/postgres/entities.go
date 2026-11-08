package postgres

import (
	"time"
)

// Rows of the version_control schema. They carry GORM's mapping so the
// domain types do not. IDs and timestamps are set by the domain, never by
// GORM.

type connectionEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	Provider             string     `gorm:"column:provider"`
	HostURL              string     `gorm:"column:host_url"`
	AuthKind             string     `gorm:"column:auth_kind"`
	CredentialRef        *string    `gorm:"column:credential_ref"`
	ExternalAccountID    string     `gorm:"column:external_account_id"`
	ExternalAccountName  *string    `gorm:"column:external_account_name"`
	LookupFrom           string     `gorm:"column:lookup_from"`
	LookupPool           *string    `gorm:"column:lookup_pool"`
	Status               string     `gorm:"column:status"`
	CreatedByPrincipalID *string    `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt            *time.Time `gorm:"column:deleted_at"`
}

func (connectionEntity) TableName() string { return "version_control.connections" }

type sourceEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	ConnectionID         string     `gorm:"column:connection_id"`
	WorkspaceID          string     `gorm:"column:workspace_id"`
	ProjectID            string     `gorm:"column:project_id"`
	ExternalRepositoryID string     `gorm:"column:external_repo_id"`
	RepositoryKey        string     `gorm:"column:repository_key"`
	FullName             string     `gorm:"column:full_name"`
	DefaultBranch        *string    `gorm:"column:default_branch"`
	Visibility           string     `gorm:"column:visibility"`
	CloneURL             *string    `gorm:"column:clone_url"`
	WebURL               *string    `gorm:"column:web_url"`
	CreatedByPrincipalID *string    `gorm:"column:created_by_principal_id"`
	UpstreamArchivedAt   *time.Time `gorm:"column:upstream_archived_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt           *time.Time `gorm:"column:archived_at"`
}

func (sourceEntity) TableName() string { return "version_control.sources" }

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
