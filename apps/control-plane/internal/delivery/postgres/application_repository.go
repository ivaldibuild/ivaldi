package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

type applicationRepository struct {
	db     *gorm.DB
	mapper *applicationMapper
}

// NewApplicationRepository returns the PostgreSQL ApplicationRepository. It
// uses the transaction carried by the context when there is one.
func NewApplicationRepository(db *gorm.DB) delivery.ApplicationRepository {
	return &applicationRepository{db: db, mapper: newApplicationMapper()}
}

func (r *applicationRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// Applications

func (r *applicationRepository) CreateApplication(ctx context.Context, a *delivery.Application) error {
	e, err := r.mapper.ToPersistence(a)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *applicationRepository) GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error) {
	return r.getApplication(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, applicationID))
}

func (r *applicationRepository) GetApplicationForUpdate(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error) {
	return r.getApplication(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, applicationID))
}

func (r *applicationRepository) GetApplicationBySlug(ctx context.Context, organizationID, projectID, slug string) (*delivery.Application, error) {
	return r.getApplication(r.conn(ctx).Where("organization_id = ? AND project_id = ? AND slug = ? AND archived_at IS NULL", organizationID, projectID, slug))
}

// GetApplicationByIdentifier matches the identifier against IDs and the
// slugs of active Applications. A slug cannot be an ID (slugs have no
// underscore), so two rows mean the slug is used in two Projects.
func (r *applicationRepository) GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*delivery.Application, error) {
	var entities []applicationEntity
	err := r.conn(ctx).
		Where("organization_id = ? AND (id = ? OR (slug = ? AND archived_at IS NULL))", organizationID, identifier, identifier).
		Limit(2).Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	switch len(entities) {
	case 0:
		return nil, fmt.Errorf("application %q: %w", identifier, delivery.ErrNotFound)
	case 1:
		return r.mapper.ToDomain(&entities[0])
	}
	return nil, fmt.Errorf("application %q: %w", identifier, delivery.ErrAmbiguous)
}

func (r *applicationRepository) getApplication(q *gorm.DB) (*delivery.Application, error) {
	var entity applicationEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&entity)
}

func (r *applicationRepository) ListApplications(ctx context.Context, query delivery.ListApplicationsQuery) (*pagination.Result[*delivery.Application], error) {
	if query.WorkspaceID == "" && query.ProjectID == "" {
		return nil, delivery.NewErrInvalidInput("listing applications needs a project or a workspace")
	}
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&applicationEntity{}).Where("organization_id = ?", query.OrganizationID)
		if query.WorkspaceID != "" {
			q = q.Where("workspace_id = ?", query.WorkspaceID)
		}
		if query.ProjectID != "" {
			q = q.Where("project_id = ?", query.ProjectID)
		}
		if query.OwnerTeamID != "" {
			q = q.Where("owner_team_id = ?", query.OwnerTeamID)
		}
		if !query.IncludeArchived {
			q = q.Where("archived_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, applicationKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, r.mapper.ToDomain)
}

func (r *applicationRepository) UpdateApplication(ctx context.Context, a *delivery.Application) error {
	e, err := r.mapper.ToPersistence(a)
	if err != nil {
		return err
	}
	return updated(r.conn(ctx).Model(&applicationEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"owner_team_id":     e.OwnerTeamID,
			"slug":              e.Slug,
			"display_name":      e.DisplayName,
			"source_id":         e.SourceID,
			"workload_selector": e.WorkloadSelector,
			"updated_at":        e.UpdatedAt,
			"archived_at":       e.ArchivedAt,
		}))
}

func (r *applicationRepository) CountActiveApplications(ctx context.Context, organizationID, projectID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&applicationEntity{}).
		Where("organization_id = ? AND project_id = ? AND archived_at IS NULL", organizationID, projectID).
		Count(&n).Error
	return int(n), translate(err)
}

func (r *applicationRepository) CountActiveApplicationsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&applicationEntity{}).
		Where("organization_id = ? AND owner_team_id = ? AND archived_at IS NULL", organizationID, teamID).
		Count(&n).Error
	return int(n), translate(err)
}

// Artifacts

func (r *applicationRepository) CreateArtifact(ctx context.Context, a *delivery.Artifact) error {
	return translate(r.conn(ctx).Create(r.mapper.ArtifactToPersistence(a)).Error)
}

func (r *applicationRepository) GetArtifact(ctx context.Context, organizationID, artifactID string) (*delivery.Artifact, error) {
	var entity artifactEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, artifactID).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ArtifactToDomain(&entity)
}

func (r *applicationRepository) ListArtifacts(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error) {
	q := r.conn(ctx).Where("organization_id = ? AND application_id = ?", organizationID, applicationID)
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	var entities []artifactEntity
	if err := q.Order("name, id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*delivery.Artifact, 0, len(entities))
	for i := range entities {
		a, err := r.mapper.ArtifactToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *applicationRepository) UpdateArtifact(ctx context.Context, a *delivery.Artifact) error {
	e := r.mapper.ArtifactToPersistence(a)
	return updated(r.conn(ctx).Model(&artifactEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"name":          e.Name,
			"artifact_name": e.ArtifactName,
			"registry_id":   e.RegistryID,
			"required":      e.Required,
			"source":        e.Source,
			"updated_at":    e.UpdatedAt,
			"archived_at":   e.ArchivedAt,
		}))
}

// Channels

func (r *applicationRepository) CreateChannel(ctx context.Context, c *delivery.Channel) error {
	rows := r.mapper.ChannelToPersistence(c)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rows.channel).Error; err != nil {
			return err
		}
		if len(rows.rules) > 0 {
			return tx.Create(&rows.rules).Error
		}
		return nil
	}))
}

func (r *applicationRepository) GetChannel(ctx context.Context, organizationID, channelID string) (*delivery.Channel, error) {
	return r.getChannel(r.conn(ctx), organizationID, "id = ?", channelID)
}

func (r *applicationRepository) GetDefaultChannel(ctx context.Context, organizationID, applicationID string) (*delivery.Channel, error) {
	return r.getChannel(r.conn(ctx), organizationID, "application_id = ? AND is_default", applicationID)
}

func (r *applicationRepository) getChannel(db *gorm.DB, organizationID, where string, args ...any) (*delivery.Channel, error) {
	var entity channelEntity
	if err := db.Where("organization_id = ?", organizationID).Where(where, args...).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	cs, err := r.withRules(db, organizationID, []channelEntity{entity})
	if err != nil {
		return nil, err
	}
	return cs[0], nil
}

func (r *applicationRepository) ListChannels(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Channel, error) {
	db := r.conn(ctx)
	q := db.Where("organization_id = ? AND application_id = ?", organizationID, applicationID)
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	var entities []channelEntity
	if err := q.Order("is_default DESC, slug, id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	return r.withRules(db, organizationID, entities)
}

// withRules loads the Channels' rules and builds them.
func (r *applicationRepository) withRules(db *gorm.DB, organizationID string, entities []channelEntity) ([]*delivery.Channel, error) {
	if len(entities) == 0 {
		return []*delivery.Channel{}, nil
	}
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	var rules []channelRuleEntity
	err := db.Where("organization_id = ? AND channel_id IN ?", organizationID, ids).Order("created_at, id").Find(&rules).Error
	if err != nil {
		return nil, translate(err)
	}
	rulesOf := map[string][]channelRuleEntity{}
	for _, rule := range rules {
		rulesOf[rule.ChannelID] = append(rulesOf[rule.ChannelID], rule)
	}
	out := make([]*delivery.Channel, 0, len(entities))
	for i := range entities {
		c, err := r.mapper.ChannelToDomain(channelRows{channel: &entities[i], rules: rulesOf[entities[i].ID]})
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// UpdateChannel writes the Channel's row and replaces its rules: kept rules
// are updated in place, and removed ones are deleted.
func (r *applicationRepository) UpdateChannel(ctx context.Context, c *delivery.Channel) error {
	rows := r.mapper.ChannelToPersistence(c)
	e := rows.channel
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&channelEntity{}).
			Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
			Updates(map[string]any{
				"lifecycle_id": e.LifecycleID,
				"slug":         e.Slug,
				"display_name": e.DisplayName,
				"description":  e.Description,
				"is_default":   e.IsDefault,
				"updated_at":   e.UpdatedAt,
				"archived_at":  e.ArchivedAt,
			})
		if err := updated(res); err != nil {
			return err
		}
		keep := make([]string, len(rows.rules))
		for i, rule := range rows.rules {
			keep[i] = rule.ID
		}
		remove := tx.Where("organization_id = ? AND channel_id = ?", e.OrganizationID, e.ID)
		if len(keep) > 0 {
			remove = remove.Where("id NOT IN ?", keep)
		}
		if err := remove.Delete(&channelRuleEntity{}).Error; err != nil {
			return err
		}
		if len(rows.rules) == 0 {
			return nil
		}
		return upsertOwned(tx, &rows.rules, len(rows.rules), "channel_rules", "channel_id",
			[]string{"kind", "artifact_id", "config", "updated_at"})
	}))
}

func (r *applicationRepository) CountActiveChannelsWithLifecycle(ctx context.Context, organizationID, lifecycleID string) (int, error) {
	var n int64
	err := r.conn(ctx).Raw(`
		SELECT count(*)
		FROM delivery.channels c
		JOIN delivery.applications a ON a.organization_id = c.organization_id AND a.id = c.application_id
		WHERE c.organization_id = ? AND c.lifecycle_id = ? AND c.archived_at IS NULL AND a.archived_at IS NULL`,
		organizationID, lifecycleID).Scan(&n).Error
	return int(n), translate(err)
}

// Placements

func (r *applicationRepository) CreatePlacement(ctx context.Context, p *delivery.Placement) error {
	e, err := r.mapper.PlacementToPersistence(p)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *applicationRepository) GetPlacement(ctx context.Context, organizationID, applicationID, targetID string) (*delivery.Placement, error) {
	var entity placementEntity
	err := r.conn(ctx).Where("organization_id = ? AND application_id = ? AND target_id = ?", organizationID, applicationID, targetID).Take(&entity).Error
	if err != nil {
		return nil, translate(err)
	}
	return r.mapper.PlacementToDomain(&entity)
}

func (r *applicationRepository) ListPlacements(ctx context.Context, organizationID, applicationID string) ([]*delivery.Placement, error) {
	var entities []placementEntity
	err := r.conn(ctx).Where("organization_id = ? AND application_id = ?", organizationID, applicationID).Order("created_at, target_id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*delivery.Placement, 0, len(entities))
	for i := range entities {
		p, err := r.mapper.PlacementToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// runsOnRow is one placement joined to its Target and Environment, and to
// the Release and Deployment that are current there.
type runsOnRow struct {
	ApplicationID       string  `gorm:"column:application_id"`
	TargetID            string  `gorm:"column:target_id"`
	TargetName          string  `gorm:"column:target_name"`
	EnvironmentID       string  `gorm:"column:environment_id"`
	EnvironmentName     string  `gorm:"column:environment_name"`
	CurrentReleaseID    *string `gorm:"column:current_release_id"`
	ReleaseVersion      *string `gorm:"column:release_version"`
	CurrentDeploymentID *string `gorm:"column:current_deployment_id"`
	DeploymentStatus    *string `gorm:"column:deployment_status"`
}

// ListRunsOn reads where the Applications run in one query over
// application_targets, joining Targets, Environments, Releases, and
// Deployments. Archived Targets and Environments are left out, so a grid
// only shows what still runs somewhere.
func (r *applicationRepository) ListRunsOn(ctx context.Context, organizationID string, applicationIDs []string) ([]delivery.PlacementRunsOn, error) {
	if len(applicationIDs) == 0 {
		return nil, nil
	}
	var rows []runsOnRow
	err := r.conn(ctx).
		Table("delivery.application_targets AS placement").
		Select(`placement.application_id,
			t.id AS target_id, t.display_name AS target_name,
			e.id AS environment_id, e.display_name AS environment_name,
			placement.current_release_id, rel.version AS release_version,
			placement.current_deployment_id, dep.status AS deployment_status`).
		Joins("JOIN delivery.targets t ON t.organization_id = placement.organization_id AND t.id = placement.target_id").
		Joins("JOIN delivery.environments e ON e.organization_id = t.organization_id AND e.id = t.environment_id").
		Joins("LEFT JOIN delivery.releases rel ON rel.organization_id = placement.organization_id AND rel.id = placement.current_release_id").
		Joins("LEFT JOIN delivery.deployments dep ON dep.organization_id = placement.organization_id AND dep.id = placement.current_deployment_id").
		Where("placement.organization_id = ? AND placement.application_id IN ?", organizationID, applicationIDs).
		Where("t.deleted_at IS NULL AND e.deleted_at IS NULL").
		Order("e.created_at, e.id, t.display_name, t.id").
		Scan(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]delivery.PlacementRunsOn, 0, len(rows))
	for _, row := range rows {
		item := delivery.PlacementRunsOn{
			ApplicationID:   row.ApplicationID,
			TargetID:        row.TargetID,
			TargetName:      row.TargetName,
			EnvironmentID:   row.EnvironmentID,
			EnvironmentName: row.EnvironmentName,
			ReleaseID:       row.CurrentReleaseID,
			ReleaseVersion:  row.ReleaseVersion,
			DeploymentID:    row.CurrentDeploymentID,
		}
		if row.DeploymentStatus != nil {
			status := release.DeploymentStatus(*row.DeploymentStatus)
			item.DeploymentStatus = &status
		}
		out = append(out, item)
	}
	return out, nil
}

// UpdatePlacement writes the placement's configuration. It leaves the
// current Deployment and Release alone: Deployments set those, and a
// configuration change must not overwrite one made concurrently.
func (r *applicationRepository) UpdatePlacement(ctx context.Context, p *delivery.Placement) error {
	e, err := r.mapper.PlacementToPersistence(p)
	if err != nil {
		return err
	}
	return updated(r.conn(ctx).Model(&placementEntity{}).
		Where("organization_id = ? AND application_id = ? AND target_id = ?", e.OrganizationID, e.ApplicationID, e.TargetID).
		Updates(map[string]any{
			"source":       e.Source,
			"apply_config": e.ApplyConfig,
			"updated_at":   e.UpdatedAt,
		}))
}

// UpdatePlacementCurrent writes only what is running now, so a configuration
// change made at the same time cannot overwrite it.
func (r *applicationRepository) UpdatePlacementCurrent(ctx context.Context, p *delivery.Placement) error {
	s := p.Snapshot()
	return updated(r.conn(ctx).Model(&placementEntity{}).
		Where("organization_id = ? AND application_id = ? AND target_id = ?", s.OrganizationID, s.ApplicationID, s.TargetID).
		Updates(map[string]any{
			"current_deployment_id": s.CurrentDeploymentID,
			"current_release_id":    s.CurrentReleaseID,
			"updated_at":            s.UpdatedAt,
		}))
}

func (r *applicationRepository) DeletePlacement(ctx context.Context, organizationID, applicationID, targetID string) error {
	res := r.conn(ctx).Where("organization_id = ? AND application_id = ? AND target_id = ?", organizationID, applicationID, targetID).Delete(&placementEntity{})
	if _, ok := database.ForeignKeyViolation(res.Error); ok {
		return fmt.Errorf("%w: deployments refer to application %s on target %s: %w", delivery.ErrInUse, applicationID, targetID, res.Error)
	}
	return updated(res)
}
