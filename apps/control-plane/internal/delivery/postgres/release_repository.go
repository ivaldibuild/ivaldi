package postgres

import (
	"context"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type buildRepository struct {
	db *gorm.DB
}

// NewBuildRepository returns the PostgreSQL BuildRepository. It uses the
// transaction carried by the context when there is one.
func NewBuildRepository(db *gorm.DB) delivery.BuildRepository {
	return &buildRepository{db: db}
}

func (r *buildRepository) conn(ctx context.Context) *gorm.DB { return database.Conn(ctx, r.db) }

// CreateBuild skips a Build whose artifact name and digest are stored, so a
// concurrent report of the same Build does not abort the caller's
// transaction; the caller reads the stored one instead.
func (r *buildRepository) CreateBuild(ctx context.Context, b *delivery.Build) (bool, error) {
	res := r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "organization_id"}, {Name: "artifact_name"}, {Name: "digest"}},
		DoNothing: true,
	}).Create(buildToPersistence(b))
	if res.Error != nil {
		return false, translate(res.Error)
	}
	return res.RowsAffected == 1, nil
}

func (r *buildRepository) GetBuild(ctx context.Context, organizationID, buildID string) (*delivery.Build, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, buildID))
}

func (r *buildRepository) GetBuildByDigestForUpdate(ctx context.Context, organizationID, artifactName, digest string) (*delivery.Build, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND artifact_name = ? AND digest = ?", organizationID, artifactName, digest))
}

func (r *buildRepository) get(q *gorm.DB) (*delivery.Build, error) {
	var e buildEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return buildToDomain(&e)
}

func (r *buildRepository) GetBuilds(ctx context.Context, organizationID string, buildIDs []string) ([]*delivery.Build, error) {
	if len(buildIDs) == 0 {
		return []*delivery.Build{}, nil
	}
	var entities []buildEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id IN ?", organizationID, buildIDs).Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*delivery.Build, len(entities))
	for i := range entities {
		b, err := buildToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func (r *buildRepository) UpdateBuild(ctx context.Context, b *delivery.Build) error {
	e := buildToPersistence(b)
	return updated(r.conn(ctx).Model(&buildEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"version":       e.Version,
			"provenance":    e.Provenance,
			"source_id":     e.SourceID,
			"commit_sha":    e.CommitSHA,
			"source_ref":    e.SourceRef,
			"ci_provider":   e.CIProvider,
			"ci_run_ref":    e.CIRunRef,
			"reported_tags": e.ReportedTags,
			"built_at":      e.BuiltAt,
			"updated_at":    e.UpdatedAt,
		}))
}

type releaseRepository struct {
	db *gorm.DB
}

// NewReleaseRepository returns the PostgreSQL ReleaseRepository. It uses the
// transaction carried by the context when there is one.
func NewReleaseRepository(db *gorm.DB) delivery.ReleaseRepository {
	return &releaseRepository{db: db}
}

func (r *releaseRepository) conn(ctx context.Context) *gorm.DB { return database.Conn(ctx, r.db) }

func (r *releaseRepository) CreateRelease(ctx context.Context, rel *delivery.Release) error {
	e, builds := releaseToPersistence(rel)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Create(&builds).Error
	}))
}

func (r *releaseRepository) GetRelease(ctx context.Context, organizationID, releaseID string) (*delivery.Release, error) {
	return r.get(r.conn(ctx), r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, releaseID))
}

func (r *releaseRepository) GetReleaseForUpdate(ctx context.Context, organizationID, releaseID string) (*delivery.Release, error) {
	return r.get(r.conn(ctx), r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, releaseID))
}

func (r *releaseRepository) GetReleaseByVersion(ctx context.Context, organizationID, applicationID, version string) (*delivery.Release, error) {
	return r.get(r.conn(ctx), r.conn(ctx).Where(
		"organization_id = ? AND application_id = ? AND version = ?", organizationID, applicationID, version))
}

func (r *releaseRepository) GetReleaseWithImages(ctx context.Context, organizationID, applicationID string, buildIDs []string) (*delivery.Release, error) {
	buildIDs = slices.Compact(slices.Sorted(slices.Values(buildIDs)))
	if len(buildIDs) == 0 {
		return nil, delivery.ErrNotFound
	}
	// Two artifacts can run one image (an api and a migrate job built from
	// the same repository), so a Release can hold a Build twice. Counting
	// distinct Builds compares sets: n of them, all among buildIDs, is an
	// exact match.
	matching := r.conn(ctx).Table("delivery.release_builds AS rb").
		Select("rb.release_id").
		Joins("JOIN delivery.application_artifacts AS a ON a.organization_id = rb.organization_id AND a.application_id = rb.application_id AND a.id = rb.artifact_id").
		Where("rb.organization_id = ? AND rb.application_id = ? AND a.artifact_kind = ?", organizationID, applicationID, string(delivery.ArtifactContainerImage)).
		Group("rb.release_id").
		Having("count(DISTINCT rb.build_id) = ? AND count(DISTINCT rb.build_id) FILTER (WHERE rb.build_id IN ?) = ?", len(buildIDs), buildIDs, len(buildIDs))
	return r.get(r.conn(ctx), r.conn(ctx).
		Where("organization_id = ? AND application_id = ? AND id IN (?)", organizationID, applicationID, matching).
		Order(clause.OrderBy{Expression: clause.Expr{SQL: "source = ? , created_at DESC, id DESC", Vars: []any{string(delivery.ReleaseObserved)}}}))
}

func (r *releaseRepository) get(db, q *gorm.DB) (*delivery.Release, error) {
	var e releaseEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	out, err := r.withBuilds(db, e.OrganizationID, []releaseEntity{e})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// withBuilds loads the Releases' Builds and builds them.
func (r *releaseRepository) withBuilds(db *gorm.DB, organizationID string, entities []releaseEntity) ([]*delivery.Release, error) {
	if len(entities) == 0 {
		return []*delivery.Release{}, nil
	}
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	var builds []releaseBuildEntity
	if err := db.Where("organization_id = ? AND release_id IN ?", organizationID, ids).Find(&builds).Error; err != nil {
		return nil, translate(err)
	}
	buildsOf := map[string][]releaseBuildEntity{}
	for _, b := range builds {
		buildsOf[b.ReleaseID] = append(buildsOf[b.ReleaseID], b)
	}
	out := make([]*delivery.Release, len(entities))
	for i := range entities {
		rel, err := releaseToDomain(&entities[i], buildsOf[entities[i].ID])
		if err != nil {
			return nil, err
		}
		out[i] = rel
	}
	return out, nil
}

func (r *releaseRepository) ListReleases(ctx context.Context, q delivery.ListReleasesQuery) (*pagination.Result[*delivery.Release], error) {
	db := r.conn(ctx)
	base := func() *gorm.DB {
		b := db.Model(&releaseEntity{}).Where("organization_id = ? AND application_id = ?", q.OrganizationID, q.ApplicationID)
		if q.ChannelID != "" {
			b = b.Where("channel_id = ?", q.ChannelID)
		}
		if q.Status != "" {
			b = b.Where("status = ?", string(q.Status))
		}
		return b
	}
	page, err := paginate(base, q.Page, releaseKeyset)
	if err != nil {
		return nil, err
	}
	releases, err := r.withBuilds(db, q.OrganizationID, page.Rows)
	if err != nil {
		return nil, err
	}
	i := 0
	return database.ToResult(page, func(*releaseEntity) (*delivery.Release, error) {
		rel := releases[i]
		i++
		return rel, nil
	})
}

func (r *releaseRepository) UpdateRelease(ctx context.Context, rel *delivery.Release) error {
	e, _ := releaseToPersistence(rel)
	return updated(r.conn(ctx).Model(&releaseEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"status":              e.Status,
			"publish_decision_id": e.PublishDecisionID,
			"published_at":        e.PublishedAt,
			"withdrawn_at":        e.WithdrawnAt,
			"updated_at":          e.UpdatedAt,
		}))
}
