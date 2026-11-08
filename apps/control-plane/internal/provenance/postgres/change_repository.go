// Package postgres implements the provenance repositories with GORM on the
// provenance schema created by ../migrations.
package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type changeRepository struct {
	db     *gorm.DB
	mapper *changeMapper
}

// NewChangeRepository returns the PostgreSQL ChangeRepository. It uses the
// transaction carried by the context when there is one; CreateChange writes
// its Change and commits together, so callers should run it in one.
func NewChangeRepository(db *gorm.DB) provenance.ChangeRepository {
	return &changeRepository{db: db, mapper: newChangeMapper()}
}

func (r *changeRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *changeRepository) CreateChange(ctx context.Context, c *provenance.Change) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(r.mapper.ToPersistence(c)).Error; err != nil {
			return translate(err)
		}
		commits := r.mapper.commits(c)
		if len(commits) == 0 {
			return nil
		}
		return translate(tx.Create(&commits).Error)
	})
}

func (r *changeRepository) GetChange(ctx context.Context, organizationID, changeID string) (*provenance.Change, error) {
	var entity changeEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, changeID).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	commits, err := r.commitSHAs(ctx, organizationID, []string{changeID})
	if err != nil {
		return nil, err
	}
	return r.mapper.ToDomain(&entity, commits[changeID])
}

// ChangesForCommits returns the Changes holding any of the commits.
func (r *changeRepository) ChangesForCommits(ctx context.Context, organizationID, sourceID string, commitSHAs []string) ([]*provenance.Change, error) {
	if len(commitSHAs) == 0 {
		return nil, nil
	}
	var changeIDs []string
	err := r.conn(ctx).Model(&changeCommitEntity{}).
		Where("organization_id = ? AND source_id = ? AND commit_sha IN ?", organizationID, sourceID, commitSHAs).
		Distinct().Pluck("change_id", &changeIDs).Error
	if err != nil {
		return nil, translate(err)
	}
	return r.changesByID(ctx, organizationID, changeIDs)
}

func (r *changeRepository) ListChanges(ctx context.Context, query provenance.ListChangesQuery) (*pagination.Result[*provenance.Change], error) {
	base := func() *gorm.DB {
		return r.conn(ctx).Model(&changeEntity{}).
			Where("organization_id = ? AND source_id = ?", query.OrganizationID, query.SourceID)
	}
	page, err := paginate(base, query.Page)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(page.Rows))
	for i := range page.Rows {
		ids = append(ids, page.Rows[i].ID)
	}
	commits, err := r.commitSHAs(ctx, query.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	items := make([]*provenance.Change, 0, len(page.Rows))
	for i := range page.Rows {
		c, err := r.mapper.ToDomain(&page.Rows[i], commits[page.Rows[i].ID])
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return pagination.NewResult(items, page.Total, page.HasNext, page.HasPrevious, page.Next, page.Previous), nil
}

// changesByID loads the Changes with the given IDs, in creation order.
func (r *changeRepository) changesByID(ctx context.Context, organizationID string, ids []string) ([]*provenance.Change, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []changeEntity
	err := r.conn(ctx).Where("organization_id = ? AND id IN ?", organizationID, ids).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	commits, err := r.commitSHAs(ctx, organizationID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*provenance.Change, 0, len(entities))
	for i := range entities {
		c, err := r.mapper.ToDomain(&entities[i], commits[entities[i].ID])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// commitSHAs returns each Change's commits, keyed by Change ID.
func (r *changeRepository) commitSHAs(ctx context.Context, organizationID string, changeIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(changeIDs) == 0 {
		return out, nil
	}
	var rows []changeCommitEntity
	err := r.conn(ctx).Where("organization_id = ? AND change_id IN ?", organizationID, changeIDs).
		Order("change_id, commit_sha").Find(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	for _, row := range rows {
		out[row.ChangeID] = append(out[row.ChangeID], row.CommitSHA)
	}
	return out, nil
}
