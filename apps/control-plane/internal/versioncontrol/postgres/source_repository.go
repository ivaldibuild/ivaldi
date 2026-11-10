package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type sourceRepository struct {
	db     *gorm.DB
	mapper *sourceMapper
}

// NewSourceRepository returns the PostgreSQL SourceRepository. It uses the
// transaction carried by the context when there is one.
func NewSourceRepository(db *gorm.DB) versioncontrol.SourceRepository {
	return &sourceRepository{db: db, mapper: newSourceMapper()}
}

func (r *sourceRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *sourceRepository) CreateSource(ctx context.Context, s *versioncontrol.Source) error {
	return translate(r.conn(ctx).Create(r.mapper.ToPersistence(s)).Error)
}

func (r *sourceRepository) GetSource(ctx context.Context, organizationID, sourceID string) (*versioncontrol.Source, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, sourceID))
}

func (r *sourceRepository) GetSourceForUpdate(ctx context.Context, organizationID, sourceID string) (*versioncontrol.Source, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, sourceID))
}

func (r *sourceRepository) GetSourceByRepositoryKey(ctx context.Context, organizationID, repositoryKey string) (*versioncontrol.Source, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND repository_key = ? AND archived_at IS NULL", organizationID, repositoryKey))
}

func (r *sourceRepository) GetSourceByRepositoryKeyForUpdate(ctx context.Context, organizationID, repositoryKey string) (*versioncontrol.Source, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND repository_key = ? AND archived_at IS NULL", organizationID, repositoryKey))
}

func (r *sourceRepository) get(q *gorm.DB) (*versioncontrol.Source, error) {
	var entity sourceEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *sourceRepository) ListSources(ctx context.Context, query versioncontrol.ListSourcesQuery) (*pagination.Result[*versioncontrol.Source], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&sourceEntity{}).Where("organization_id = ?", query.OrganizationID)
		if query.ProjectID != "" {
			q = q.Where("project_id = ?", query.ProjectID)
		}
		if query.WorkspaceID != "" {
			q = q.Where("workspace_id = ?", query.WorkspaceID)
		}
		if !query.IncludeArchived {
			q = q.Where("archived_at IS NULL")
		}
		return q
	}
	page, err := paginate(base, query.Page)
	if err != nil {
		return nil, err
	}
	return database.ToResult(page, r.mapper.ToDomain)
}

func (r *sourceRepository) UpdateSource(ctx context.Context, s *versioncontrol.Source) error {
	e := r.mapper.ToPersistence(s)
	return updated(r.conn(ctx).Model(&sourceEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"full_name":            e.FullName,
			"default_branch":       e.DefaultBranch,
			"visibility":           e.Visibility,
			"clone_url":            e.CloneURL,
			"web_url":              e.WebURL,
			"upstream_archived_at": e.UpstreamArchivedAt,
			"provider_updated_at":  e.ProviderUpdatedAt,
			"updated_at":           e.UpdatedAt,
			"archived_at":          e.ArchivedAt,
		}))
}

func (r *sourceRepository) ConnectedRepositoryKeys(ctx context.Context, organizationID string, keys []string) ([]string, error) {
	connected := []string{}
	if len(keys) == 0 {
		return connected, nil
	}
	err := r.conn(ctx).Model(&sourceEntity{}).
		Where("organization_id = ? AND repository_key IN ? AND archived_at IS NULL", organizationID, keys).
		Pluck("repository_key", &connected).Error
	return connected, translate(err)
}
