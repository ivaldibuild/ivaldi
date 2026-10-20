package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type lifecycleRepository struct {
	db     *gorm.DB
	mapper *lifecycleMapper
}

// NewLifecycleRepository returns the PostgreSQL LifecycleRepository. It
// uses the transaction carried by the context when there is one.
func NewLifecycleRepository(db *gorm.DB) delivery.LifecycleRepository {
	return &lifecycleRepository{db: db, mapper: newLifecycleMapper()}
}

func (r *lifecycleRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *lifecycleRepository) CreateLifecycle(ctx context.Context, l *delivery.Lifecycle) error {
	rows := r.mapper.ToPersistence(l)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rows.lifecycle).Error; err != nil {
			return err
		}
		return writePhases(tx, rows)
	}))
}

// writePhases inserts a Lifecycle's phases and their environments.
func writePhases(tx *gorm.DB, rows lifecycleRows) error {
	if len(rows.phases) > 0 {
		if err := tx.Create(&rows.phases).Error; err != nil {
			return err
		}
	}
	if len(rows.environments) > 0 {
		if err := tx.Create(&rows.environments).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *lifecycleRepository) GetLifecycle(ctx context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	return r.get(r.conn(ctx), organizationID, false, "id = ?", lifecycleID)
}

func (r *lifecycleRepository) GetLifecycleForUpdate(ctx context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	return r.get(r.conn(ctx), organizationID, true, "id = ?", lifecycleID)
}

func (r *lifecycleRepository) GetLifecycleBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*delivery.Lifecycle, error) {
	return r.get(r.conn(ctx), organizationID, false, "workspace_id = ? AND slug = ? AND deleted_at IS NULL", workspaceID, slug)
}

func (r *lifecycleRepository) get(db *gorm.DB, organizationID string, lock bool, where string, args ...any) (*delivery.Lifecycle, error) {
	q := db.Where("organization_id = ?", organizationID).Where(where, args...)
	if lock {
		q = q.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
	}
	var entity lifecycleEntity
	if err := q.Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	ls, err := r.withPhases(db, organizationID, []lifecycleEntity{entity})
	if err != nil {
		return nil, err
	}
	return ls[0], nil
}

// withPhases loads the phases and environments of the Lifecycles and builds them.
func (r *lifecycleRepository) withPhases(db *gorm.DB, organizationID string, entities []lifecycleEntity) ([]*delivery.Lifecycle, error) {
	if len(entities) == 0 {
		return []*delivery.Lifecycle{}, nil
	}
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	var phases []lifecyclePhaseEntity
	err := db.Where("organization_id = ? AND lifecycle_id IN ?", organizationID, ids).Order("lifecycle_id, position").Find(&phases).Error
	if err != nil {
		return nil, translate(err)
	}
	var envs []lifecyclePhaseEnvironmentEntity
	err = db.Where("organization_id = ? AND lifecycle_id IN ?", organizationID, ids).Order("phase_id, environment_id").Find(&envs).Error
	if err != nil {
		return nil, translate(err)
	}
	phasesOf := map[string][]lifecyclePhaseEntity{}
	for _, p := range phases {
		phasesOf[p.LifecycleID] = append(phasesOf[p.LifecycleID], p)
	}
	envsOf := map[string][]lifecyclePhaseEnvironmentEntity{}
	for _, e := range envs {
		envsOf[e.LifecycleID] = append(envsOf[e.LifecycleID], e)
	}
	out := make([]*delivery.Lifecycle, 0, len(entities))
	for i := range entities {
		l, err := r.mapper.ToDomain(lifecycleRows{lifecycle: &entities[i], phases: phasesOf[entities[i].ID], environments: envsOf[entities[i].ID]})
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (r *lifecycleRepository) ListLifecycles(ctx context.Context, query delivery.ListLifecyclesQuery) (*pagination.Result[*delivery.Lifecycle], error) {
	db := r.conn(ctx)
	base := func() *gorm.DB {
		q := db.Model(&lifecycleEntity{}).Where("organization_id = ? AND workspace_id = ?", query.OrganizationID, query.WorkspaceID)
		if !query.IncludeArchived {
			q = q.Where("deleted_at IS NULL")
		}
		return q
	}
	p, err := paginate(base, query.Page, lifecycleKeyset)
	if err != nil {
		return nil, err
	}
	items, err := r.withPhases(db, query.OrganizationID, p.Rows)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(items, p.Total, p.HasNext, p.HasPrevious, p.Next, p.Previous), nil
}

// UpdateLifecycle writes the Lifecycle's row and replaces its phases: kept
// phases are updated in place, so their IDs and creation times survive, and
// removed ones are deleted. Phase positions may collide until the
// transaction commits, which the deferred unique constraint allows.
func (r *lifecycleRepository) UpdateLifecycle(ctx context.Context, l *delivery.Lifecycle) error {
	rows := r.mapper.ToPersistence(l)
	e := rows.lifecycle
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&lifecycleEntity{}).
			Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
			Updates(map[string]any{
				"slug":         e.Slug,
				"display_name": e.DisplayName,
				"description":  e.Description,
				"updated_at":   e.UpdatedAt,
				"deleted_at":   e.DeletedAt,
			})
		if err := updated(res); err != nil {
			return err
		}
		scope := tx.Where("organization_id = ? AND lifecycle_id = ?", e.OrganizationID, e.ID)
		// Environments go first: each may be in only one phase at a time, and
		// that is checked immediately.
		if err := scope.Session(&gorm.Session{}).Delete(&lifecyclePhaseEnvironmentEntity{}).Error; err != nil {
			return err
		}
		keep := make([]string, len(rows.phases))
		for i, p := range rows.phases {
			keep[i] = p.ID
		}
		remove := scope.Session(&gorm.Session{})
		if len(keep) > 0 {
			remove = remove.Where("id NOT IN ?", keep)
		}
		if err := remove.Delete(&lifecyclePhaseEntity{}).Error; err != nil {
			return err
		}
		if len(rows.phases) > 0 {
			err := upsertOwned(tx, &rows.phases, len(rows.phases), "lifecycle_phases", "lifecycle_id",
				[]string{"position", "name", "optional", "min_environments", "updated_at"})
			if err != nil {
				return err
			}
		}
		if len(rows.environments) > 0 {
			return tx.Create(&rows.environments).Error
		}
		return nil
	}))
}

func (r *lifecycleRepository) CountActiveLifecycles(ctx context.Context, organizationID, workspaceID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&lifecycleEntity{}).
		Where("organization_id = ? AND workspace_id = ? AND deleted_at IS NULL", organizationID, workspaceID).
		Count(&n).Error
	return int(n), translate(err)
}

func (r *lifecycleRepository) CountActiveLifecyclesWithEnvironment(ctx context.Context, organizationID, environmentID string) (int, error) {
	var n int64
	err := r.conn(ctx).Raw(`
		SELECT count(DISTINCT l.id)
		FROM delivery.lifecycles l
		JOIN delivery.lifecycle_phase_environments pe
		  ON pe.organization_id = l.organization_id AND pe.lifecycle_id = l.id
		WHERE l.organization_id = ? AND pe.environment_id = ? AND l.deleted_at IS NULL`,
		organizationID, environmentID).Scan(&n).Error
	return int(n), translate(err)
}
