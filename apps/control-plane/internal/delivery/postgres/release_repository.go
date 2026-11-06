package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

type buildEntity struct {
	ID             string             `gorm:"column:id;primaryKey"`
	OrganizationID string             `gorm:"column:organization_id"`
	ArtifactKind   string             `gorm:"column:artifact_kind"`
	ArtifactName   string             `gorm:"column:artifact_name"`
	Digest         string             `gorm:"column:digest"`
	Version        *string            `gorm:"column:version"`
	Provenance     string             `gorm:"column:provenance"`
	SourceID       *string            `gorm:"column:source_id"`
	CommitSHA      *string            `gorm:"column:commit_sha"`
	SourceRef      *string            `gorm:"column:source_ref"`
	CIProvider     *string            `gorm:"column:ci_provider"`
	CIRunRef       *string            `gorm:"column:ci_run_ref"`
	ReportedTags   database.TextArray `gorm:"column:reported_tags"`
	BuiltAt        *time.Time         `gorm:"column:built_at"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (buildEntity) TableName() string { return "delivery.builds" }

type releaseEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	ApplicationID        string     `gorm:"column:application_id"`
	ChannelID            string     `gorm:"column:channel_id"`
	Version              string     `gorm:"column:version"`
	Status               string     `gorm:"column:status"`
	Source               string     `gorm:"column:source"`
	CreatedByPrincipalID *string    `gorm:"column:created_by_principal_id"`
	PublishDecisionID    *string    `gorm:"column:publish_decision_id"`
	PublishedAt          *time.Time `gorm:"column:published_at"`
	WithdrawnAt          *time.Time `gorm:"column:withdrawn_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (releaseEntity) TableName() string { return "delivery.releases" }

type releaseBuildEntity struct {
	OrganizationID string    `gorm:"column:organization_id"`
	ApplicationID  string    `gorm:"column:application_id"`
	ReleaseID      string    `gorm:"column:release_id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;primaryKey"`
	BuildID        string    `gorm:"column:build_id"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

func (releaseBuildEntity) TableName() string { return "delivery.release_builds" }

func buildToPersistence(b *delivery.Build) *buildEntity {
	s := b.Snapshot()
	return &buildEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, ArtifactKind: string(s.ArtifactKind), ArtifactName: s.ArtifactName,
		Digest: s.Digest, Version: s.Version, Provenance: string(s.Provenance), SourceID: s.SourceID,
		CommitSHA: s.CommitSHA, SourceRef: s.SourceRef, CIProvider: s.CIProvider, CIRunRef: s.CIRunRef,
		ReportedTags: s.ReportedTags, BuiltAt: s.BuiltAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func buildToDomain(e *buildEntity) (*delivery.Build, error) {
	return delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: e.OrganizationID,
		ArtifactKind:   delivery.ArtifactKind(e.ArtifactKind),
		ArtifactName:   e.ArtifactName,
		Digest:         e.Digest,
		Provenance:     delivery.BuildProvenance(e.Provenance),
		Details: delivery.BuildDetails{
			Version: e.Version, SourceID: e.SourceID, CommitSHA: e.CommitSHA, SourceRef: e.SourceRef,
			CIProvider: e.CIProvider, CIRunRef: e.CIRunRef, ReportedTags: e.ReportedTags, BuiltAt: utcPtr(e.BuiltAt),
		},
		CreatedAt: utc(e.CreatedAt),
		UpdatedAt: utc(e.UpdatedAt),
	}, &e.ID)
}

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

func releaseToPersistence(rel *delivery.Release) (*releaseEntity, []releaseBuildEntity) {
	s := rel.Snapshot()
	e := &releaseEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, ChannelID: s.ChannelID,
		Version: s.Version, Status: string(s.Status), Source: string(s.Source),
		CreatedByPrincipalID: s.CreatedByPrincipalID, PublishDecisionID: s.PublishDecisionID,
		PublishedAt: s.PublishedAt, WithdrawnAt: s.WithdrawnAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	builds := make([]releaseBuildEntity, len(s.Builds))
	for i, b := range s.Builds {
		builds[i] = releaseBuildEntity{
			OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, ReleaseID: s.ID,
			ArtifactID: b.ArtifactID, BuildID: b.BuildID, CreatedAt: b.CreatedAt,
		}
	}
	return e, builds
}

func releaseToDomain(e *releaseEntity, builds []releaseBuildEntity) (*delivery.Release, error) {
	props := delivery.NewReleaseProps{
		OrganizationID: e.OrganizationID, ApplicationID: e.ApplicationID, ChannelID: e.ChannelID,
		Version: e.Version, Status: release.Status(e.Status), Source: delivery.ReleaseSource(e.Source),
		CreatedByPrincipalID: e.CreatedByPrincipalID, PublishDecisionID: e.PublishDecisionID,
		PublishedAt: utcPtr(e.PublishedAt), WithdrawnAt: utcPtr(e.WithdrawnAt),
		CreatedAt: utc(e.CreatedAt), UpdatedAt: utc(e.UpdatedAt),
	}
	for _, b := range builds {
		props.Builds = append(props.Builds, delivery.NewReleaseBuildProps{ArtifactID: b.ArtifactID, BuildID: b.BuildID, CreatedAt: utc(b.CreatedAt)})
	}
	return delivery.NewRelease(props, &e.ID)
}

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
