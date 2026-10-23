package postgres

import (
	"encoding/json"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// Rows of the workspace schema. They carry GORM's mapping so the domain types
// do not. IDs and timestamps are set by the domain, never by GORM.

type workspaceEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	Slug                 string     `gorm:"column:slug"`
	DisplayName          string     `gorm:"column:display_name"`
	Description          *string    `gorm:"column:description"`
	IsDefault            bool       `gorm:"column:is_default"`
	CreatedByPrincipalID *string    `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt           *time.Time `gorm:"column:archived_at"`
}

func (workspaceEntity) TableName() string { return "workspace.workspaces" }

type projectEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	WorkspaceID          string     `gorm:"column:workspace_id"`
	OwnerTeamID          string     `gorm:"column:owner_team_id"`
	Slug                 string     `gorm:"column:slug"`
	DisplayName          string     `gorm:"column:display_name"`
	Description          *string    `gorm:"column:description"`
	CreatedByPrincipalID *string    `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt           *time.Time `gorm:"column:archived_at"`
}

func (projectEntity) TableName() string { return "workspace.projects" }

// settingEntity has no primary key: the level and key identify a row.
type settingEntity struct {
	OrganizationID       string    `gorm:"column:organization_id"`
	WorkspaceID          *string   `gorm:"column:workspace_id"`
	ProjectID            *string   `gorm:"column:project_id"`
	Key                  string    `gorm:"column:key"`
	Value                []byte    `gorm:"column:value;type:jsonb"`
	Version              int64     `gorm:"column:version"`
	UpdatedByPrincipalID *string   `gorm:"column:updated_by_principal_id"`
	PolicyDecisionID     *string   `gorm:"column:policy_decision_id"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (settingEntity) TableName() string { return "workspace.settings" }

func toWorkspaceEntity(w *workspace.Workspace) *workspaceEntity {
	d := w.Snapshot()
	return &workspaceEntity{
		ID:                   d.ID,
		OrganizationID:       d.OrganizationID,
		Slug:                 d.Slug,
		DisplayName:          d.DisplayName,
		Description:          nullable(d.Description),
		IsDefault:            d.IsDefault,
		CreatedByPrincipalID: nullable(d.CreatedByPrincipalID),
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
		ArchivedAt:           d.ArchivedAt,
	}
}

func (e *workspaceEntity) toDomain() (*workspace.Workspace, error) {
	return workspace.NewWorkspace(workspace.NewWorkspaceProps{
		OrganizationID:       e.OrganizationID,
		Slug:                 e.Slug,
		DisplayName:          e.DisplayName,
		Description:          value(e.Description),
		IsDefault:            e.IsDefault,
		CreatedByPrincipalID: value(e.CreatedByPrincipalID),
		CreatedAt:            utcTime(e.CreatedAt),
		UpdatedAt:            utcTime(e.UpdatedAt),
		ArchivedAt:           utc(e.ArchivedAt),
	}, &e.ID)
}

func toProjectEntity(p *workspace.Project) *projectEntity {
	d := p.Snapshot()
	return &projectEntity{
		ID:                   d.ID,
		OrganizationID:       d.OrganizationID,
		WorkspaceID:          d.WorkspaceID,
		OwnerTeamID:          d.OwnerTeamID,
		Slug:                 d.Slug,
		DisplayName:          d.DisplayName,
		Description:          nullable(d.Description),
		CreatedByPrincipalID: nullable(d.CreatedByPrincipalID),
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
		ArchivedAt:           d.ArchivedAt,
	}
}

func (e *projectEntity) toDomain() (*workspace.Project, error) {
	return workspace.NewProject(workspace.NewProjectProps{
		OrganizationID:       e.OrganizationID,
		WorkspaceID:          e.WorkspaceID,
		OwnerTeamID:          e.OwnerTeamID,
		Slug:                 e.Slug,
		DisplayName:          e.DisplayName,
		Description:          value(e.Description),
		CreatedByPrincipalID: value(e.CreatedByPrincipalID),
		CreatedAt:            utcTime(e.CreatedAt),
		UpdatedAt:            utcTime(e.UpdatedAt),
		ArchivedAt:           utc(e.ArchivedAt),
	}, &e.ID)
}

func toSettingEntity(s *workspace.Setting) *settingEntity {
	d := s.Snapshot()
	return &settingEntity{
		OrganizationID:       d.Scope.OrganizationID,
		WorkspaceID:          nullable(d.Scope.WorkspaceID),
		ProjectID:            nullable(d.Scope.ProjectID),
		Key:                  d.Key,
		Value:                d.Value,
		Version:              d.Version,
		UpdatedByPrincipalID: nullable(d.UpdatedByPrincipalID),
		PolicyDecisionID:     nullable(d.PolicyDecisionID),
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
	}
}

func (e *settingEntity) toDomain() (*workspace.Setting, error) {
	return workspace.NewSetting(workspace.NewSettingProps{
		Scope: workspace.Scope{
			OrganizationID: e.OrganizationID,
			WorkspaceID:    value(e.WorkspaceID),
			ProjectID:      value(e.ProjectID),
		},
		Key:                  e.Key,
		Value:                json.RawMessage(e.Value),
		Version:              e.Version,
		UpdatedByPrincipalID: value(e.UpdatedByPrincipalID),
		PolicyDecisionID:     value(e.PolicyDecisionID),
		CreatedAt:            utcTime(e.CreatedAt),
		UpdatedAt:            utcTime(e.UpdatedAt),
	})
}

// nullable maps an empty string to SQL NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// utcTime returns a stored timestamp in UTC, for New…Props.
func utcTime(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
