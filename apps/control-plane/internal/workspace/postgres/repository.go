// Package postgres implements workspace.Repository with GORM on the
// workspace schema created by ../migrations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// Repository is the PostgreSQL workspace.Repository. It uses the transaction
// carried by the context when there is one (see database.Transactor).
type Repository struct {
	db *gorm.DB
}

var _ workspace.Repository = (*Repository)(nil)

// NewRepository returns a Repository on db.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// translate maps PostgreSQL errors to workspace errors by constraint name.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return workspace.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		switch c {
		case "idx_workspaces_slug", "idx_projects_slug":
			return fmt.Errorf("%w: %w", workspace.ErrSlugTaken, err)
		default:
			return fmt.Errorf("%w: %w", workspace.ErrConflict, err)
		}
	}
	if c, ok := database.CheckViolation(err); ok && c == "chk_workspaces_default_not_archived" {
		return fmt.Errorf("%w: %w", workspace.ErrDefaultWorkspace, err)
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", workspace.ErrNotFound, err)
	}
	return err
}

func (r *Repository) CreateWorkspace(ctx context.Context, w *workspace.Workspace) error {
	return translate(r.conn(ctx).Create(toWorkspaceEntity(w)).Error)
}

func (r *Repository) GetWorkspace(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return r.getWorkspace(r.conn(ctx), organizationID, workspaceID)
}

func (r *Repository) GetWorkspaceForUpdate(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return r.getWorkspace(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}), organizationID, workspaceID)
}

func (r *Repository) getWorkspace(db *gorm.DB, organizationID, workspaceID string) (*workspace.Workspace, error) {
	var rec workspaceEntity
	err := db.Where("organization_id = ? AND id = ?", organizationID, workspaceID).Take(&rec).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain()
}

func (r *Repository) GetDefaultWorkspace(ctx context.Context, organizationID string) (*workspace.Workspace, error) {
	var rec workspaceEntity
	err := r.conn(ctx).Where("organization_id = ? AND is_default", organizationID).Take(&rec).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain()
}

func (r *Repository) ListWorkspaces(ctx context.Context, organizationID string, includeArchived bool) ([]*workspace.Workspace, error) {
	q := r.conn(ctx).Where("organization_id = ?", organizationID)
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	var recs []workspaceEntity
	if err := q.Order("slug, id").Find(&recs).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*workspace.Workspace, len(recs))
	for i := range recs {
		d, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

func (r *Repository) UpdateWorkspace(ctx context.Context, w *workspace.Workspace) error {
	rec := toWorkspaceEntity(w)
	res := r.conn(ctx).Model(&workspaceEntity{}).
		Where("organization_id = ? AND id = ?", rec.OrganizationID, rec.ID).
		Updates(map[string]any{
			"slug":         rec.Slug,
			"display_name": rec.DisplayName,
			"description":  rec.Description,
			"is_default":   rec.IsDefault,
			"archived_at":  rec.ArchivedAt,
			"updated_at":   rec.UpdatedAt,
		})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return workspace.ErrNotFound
	}
	return nil
}

func (r *Repository) CountActiveProjects(ctx context.Context, organizationID, workspaceID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&projectEntity{}).
		Where("organization_id = ? AND workspace_id = ? AND archived_at IS NULL", organizationID, workspaceID).
		Count(&n).Error
	return int(n), translate(err)
}

func (r *Repository) CountActiveProjectsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&projectEntity{}).
		Where("organization_id = ? AND owner_team_id = ? AND archived_at IS NULL", organizationID, teamID).
		Count(&n).Error
	return int(n), translate(err)
}

func (r *Repository) CreateProject(ctx context.Context, p *workspace.Project) error {
	return translate(r.conn(ctx).Create(toProjectEntity(p)).Error)
}

func (r *Repository) GetProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error) {
	var rec projectEntity
	err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, projectID).Take(&rec).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain()
}

// projectKeyset sorts Projects by "created_at" (the default), "slug", or
// "display_name", with the ID breaking ties.
var projectKeyset = database.Keyset[projectEntity]{
	Keys: map[string]database.SortKey[projectEntity]{
		"created_at":   database.TimeKey("created_at", func(e *projectEntity) time.Time { return e.CreatedAt }),
		"slug":         database.StringKey("slug", func(e *projectEntity) string { return e.Slug }),
		"display_name": database.StringKey("display_name", func(e *projectEntity) string { return e.DisplayName }),
	},
	Default: "created_at",
	ID:      func(e *projectEntity) string { return e.ID },
}

func (r *Repository) ListProjects(ctx context.Context, q workspace.ListProjectsQuery) (*pagination.Result[*workspace.Project], error) {
	base := func() *gorm.DB {
		db := r.conn(ctx).Model(&projectEntity{}).
			Where("organization_id = ? AND workspace_id = ?", q.OrganizationID, q.WorkspaceID)
		if !q.IncludeArchived {
			db = db.Where("archived_at IS NULL")
		}
		if q.OwnerTeamID != "" {
			db = db.Where("owner_team_id = ?", q.OwnerTeamID)
		}
		if q.Search != "" {
			pattern := "%" + escapeLike(q.Search) + "%"
			db = db.Where(`(display_name ILIKE ? ESCAPE '\' OR slug ILIKE ? ESCAPE '\')`, pattern, pattern)
		}
		return db
	}
	page, err := database.Paginate(base, q.Page, projectKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, fmt.Errorf("%w: %w", workspace.ErrInvalid, err)
	}
	if err != nil {
		return nil, translate(err)
	}
	return database.ToResult(page, func(e *projectEntity) (*workspace.Project, error) { return e.toDomain() })
}

// escapeLike makes s match itself in a LIKE pattern that uses '\' as its
// escape character.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *Repository) UpdateProject(ctx context.Context, p *workspace.Project) error {
	rec := toProjectEntity(p)
	res := r.conn(ctx).Model(&projectEntity{}).
		Where("organization_id = ? AND id = ?", rec.OrganizationID, rec.ID).
		Updates(map[string]any{
			"slug":          rec.Slug,
			"display_name":  rec.DisplayName,
			"description":   rec.Description,
			"owner_team_id": rec.OwnerTeamID,
			"archived_at":   rec.ArchivedAt,
			"updated_at":    rec.UpdatedAt,
		})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return workspace.ErrNotFound
	}
	return nil
}

// atLevel restricts a query to exactly the scope's level. The level is
// implied by which IDs are NULL, so NULLs are matched explicitly.
func atLevel(db *gorm.DB, s workspace.Scope) *gorm.DB {
	db = db.Where("organization_id = ?", s.OrganizationID)
	if s.WorkspaceID == "" {
		db = db.Where("workspace_id IS NULL")
	} else {
		db = db.Where("workspace_id = ?", s.WorkspaceID)
	}
	if s.ProjectID == "" {
		db = db.Where("project_id IS NULL")
	} else {
		db = db.Where("project_id = ?", s.ProjectID)
	}
	return db
}

func (r *Repository) GetSetting(ctx context.Context, scope workspace.Scope, key string) (*workspace.Setting, error) {
	var rec settingEntity
	err := atLevel(r.conn(ctx), scope).Where("key = ?", key).Take(&rec).Error
	if err != nil {
		return nil, translate(err)
	}
	return rec.toDomain()
}

func (r *Repository) ListSettings(ctx context.Context, scope workspace.Scope) ([]*workspace.Setting, error) {
	var recs []settingEntity
	if err := atLevel(r.conn(ctx), scope).Order("key").Find(&recs).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*workspace.Setting, len(recs))
	for i := range recs {
		d, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

func (r *Repository) SettingsForResolution(ctx context.Context, scope workspace.Scope, key string) ([]*workspace.Setting, error) {
	q := r.conn(ctx).Where("organization_id = ? AND key = ?", scope.OrganizationID, key)
	if scope.WorkspaceID == "" {
		q = q.Where("workspace_id IS NULL")
	} else {
		q = q.Where("workspace_id IS NULL OR workspace_id = ?", scope.WorkspaceID)
	}
	if scope.ProjectID == "" {
		q = q.Where("project_id IS NULL")
	} else {
		q = q.Where("project_id IS NULL OR project_id = ?", scope.ProjectID)
	}
	var recs []settingEntity
	if err := q.Find(&recs).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*workspace.Setting, len(recs))
	for i := range recs {
		d, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

func (r *Repository) InsertSetting(ctx context.Context, s *workspace.Setting) error {
	return translate(r.conn(ctx).Create(toSettingEntity(s)).Error)
}

func (r *Repository) UpdateSetting(ctx context.Context, s *workspace.Setting, expectedVersion int64) error {
	rec := toSettingEntity(s)
	res := atLevel(r.conn(ctx).Model(&settingEntity{}), s.Scope()).
		Where("key = ? AND version = ?", rec.Key, expectedVersion).
		Updates(map[string]any{
			"value":                   rec.Value,
			"version":                 gorm.Expr("version + 1"),
			"updated_by_principal_id": rec.UpdatedByPrincipalID,
			"policy_decision_id":      rec.PolicyDecisionID,
			"updated_at":              rec.UpdatedAt,
		})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("setting %s is no longer at version %d: %w", rec.Key, expectedVersion, workspace.ErrConflict)
	}
	return nil
}

func (r *Repository) DeleteSetting(ctx context.Context, scope workspace.Scope, key string, expectedVersion int64) error {
	res := atLevel(r.conn(ctx), scope).Where("key = ? AND version = ?", key, expectedVersion).Delete(&settingEntity{})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("setting %s is no longer at version %d: %w", key, expectedVersion, workspace.ErrConflict)
	}
	return nil
}
