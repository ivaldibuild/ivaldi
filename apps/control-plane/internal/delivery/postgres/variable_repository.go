package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type variableRepository struct {
	db     *gorm.DB
	mapper *variableMapper
}

// NewVariableRepository returns the PostgreSQL VariableRepository. It uses
// the transaction carried by the context when there is one.
func NewVariableRepository(db *gorm.DB) delivery.VariableRepository {
	return &variableRepository{db: db, mapper: newVariableMapper()}
}

func (r *variableRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *variableRepository) CreateVariable(ctx context.Context, v *delivery.Variable) error {
	rows := r.mapper.ToPersistence(v)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rows.variable).Error; err != nil {
			return err
		}
		if len(rows.values) > 0 {
			return tx.Create(&rows.values).Error
		}
		return nil
	}))
}

func (r *variableRepository) GetVariable(ctx context.Context, organizationID, variableID string) (*delivery.Variable, error) {
	db := r.conn(ctx)
	var entity variableEntity
	if err := db.Where("organization_id = ? AND id = ?", organizationID, variableID).Take(&entity).Error; err != nil {
		return nil, translate(err)
	}
	vs, err := r.withValues(db, organizationID, []variableEntity{entity})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

func (r *variableRepository) ListVariables(ctx context.Context, organizationID string, owner delivery.VariableOwner) ([]*delivery.Variable, error) {
	db := r.conn(ctx)
	q := db.Where("organization_id = ? AND archived_at IS NULL", organizationID)
	if owner.ApplicationID != "" {
		q = q.Where("application_id = ?", owner.ApplicationID)
	} else {
		q = q.Where("project_id = ?", owner.ProjectID)
	}
	var entities []variableEntity
	if err := q.Order("lower(name), id").Find(&entities).Error; err != nil {
		return nil, translate(err)
	}
	return r.withValues(db, organizationID, entities)
}

func (r *variableRepository) ListVariablesInProject(ctx context.Context, organizationID, projectID string) ([]*delivery.Variable, map[string][]*delivery.Variable, error) {
	db := r.conn(ctx)
	var entities []variableEntity
	err := db.Raw(`
		SELECT v.*
		FROM delivery.variables v
		LEFT JOIN delivery.applications a
		  ON a.organization_id = v.organization_id AND a.id = v.application_id
		WHERE v.organization_id = ? AND v.archived_at IS NULL
		  AND (v.project_id = ? OR (a.project_id = ? AND a.archived_at IS NULL))
		ORDER BY lower(v.name), v.id`,
		organizationID, projectID, projectID).Scan(&entities).Error
	if err != nil {
		return nil, nil, translate(err)
	}
	all, err := r.withValues(db, organizationID, entities)
	if err != nil {
		return nil, nil, err
	}
	project := []*delivery.Variable{}
	applications := map[string][]*delivery.Variable{}
	for _, v := range all {
		if applicationID := v.ApplicationID(); applicationID != nil {
			applications[*applicationID] = append(applications[*applicationID], v)
		} else {
			project = append(project, v)
		}
	}
	return project, applications, nil
}

// withValues loads the Variables' values and builds them.
func (r *variableRepository) withValues(db *gorm.DB, organizationID string, entities []variableEntity) ([]*delivery.Variable, error) {
	if len(entities) == 0 {
		return []*delivery.Variable{}, nil
	}
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	var values []variableValueEntity
	err := db.Where("organization_id = ? AND variable_id IN ?", organizationID, ids).Order("created_at, id").Find(&values).Error
	if err != nil {
		return nil, translate(err)
	}
	valuesOf := map[string][]variableValueEntity{}
	for _, value := range values {
		valuesOf[value.VariableID] = append(valuesOf[value.VariableID], value)
	}
	out := make([]*delivery.Variable, 0, len(entities))
	for i := range entities {
		v, err := r.mapper.ToDomain(variableRows{variable: &entities[i], values: valuesOf[entities[i].ID]})
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// UpdateVariable writes the Variable's row and replaces its values: kept
// values are updated in place, so their IDs and creation times survive, and
// removed ones are deleted.
func (r *variableRepository) UpdateVariable(ctx context.Context, v *delivery.Variable) error {
	rows := r.mapper.ToPersistence(v)
	e := rows.variable
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&variableEntity{}).
			Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
			Updates(map[string]any{
				"description": e.Description,
				"values_path": e.ValuesPath,
				"deliver_as":  e.DeliverAs,
				"updated_at":  e.UpdatedAt,
				"archived_at": e.ArchivedAt,
			})
		if err := updated(res); err != nil {
			return err
		}
		keep := make([]string, len(rows.values))
		for i, value := range rows.values {
			keep[i] = value.ID
		}
		remove := tx.Where("organization_id = ? AND variable_id = ?", e.OrganizationID, e.ID)
		if len(keep) > 0 {
			remove = remove.Where("id NOT IN ?", keep)
		}
		if err := remove.Delete(&variableValueEntity{}).Error; err != nil {
			return err
		}
		if len(rows.values) == 0 {
			return nil
		}
		return upsertOwned(tx, &rows.values, len(rows.values), "variable_values", "variable_id",
			[]string{"text_value", "store_id", "path", "field", "version", "environment_ids", "target_ids", "channel_ids", "updated_at"})
	}))
}

func (r *variableRepository) CountActiveVariablesWithEnvironment(ctx context.Context, organizationID, environmentID string) (int, error) {
	return r.countScopedTo(ctx, "environment_ids", organizationID, environmentID)
}

func (r *variableRepository) CountActiveVariablesWithTarget(ctx context.Context, organizationID, targetID string) (int, error) {
	return r.countScopedTo(ctx, "target_ids", organizationID, targetID)
}

func (r *variableRepository) CountActiveVariablesWithChannel(ctx context.Context, organizationID, channelID string) (int, error) {
	return r.countScopedTo(ctx, "channel_ids", organizationID, channelID)
}

func (r *variableRepository) CountActiveVariablesWithSecretStore(ctx context.Context, organizationID, storeID string) (int, error) {
	var n int64
	err := r.conn(ctx).Raw(`
		SELECT count(DISTINCT v.id)
		FROM delivery.variable_values vv
		JOIN delivery.variables v
		  ON v.organization_id = vv.organization_id AND v.id = vv.variable_id
		LEFT JOIN delivery.applications a
		  ON a.organization_id = v.organization_id AND a.id = v.application_id
		WHERE vv.organization_id = ? AND vv.store_id = ?
		  AND v.archived_at IS NULL AND (v.application_id IS NULL OR a.archived_at IS NULL)`,
		organizationID, storeID).Scan(&n).Error
	return int(n), translate(err)
}

// countScopedTo counts the active variables with a value naming id in one
// scope column. An archived Application's variables do not count.
func (r *variableRepository) countScopedTo(ctx context.Context, column, organizationID, id string) (int, error) {
	var n int64
	err := r.conn(ctx).Raw(`
		SELECT count(DISTINCT v.id)
		FROM delivery.variable_values vv
		JOIN delivery.variables v
		  ON v.organization_id = vv.organization_id AND v.id = vv.variable_id
		LEFT JOIN delivery.applications a
		  ON a.organization_id = v.organization_id AND a.id = v.application_id
		WHERE vv.organization_id = ? AND vv.`+column+` @> ARRAY[?]::text[]
		  AND v.archived_at IS NULL AND (v.application_id IS NULL OR a.archived_at IS NULL)`,
		organizationID, id).Scan(&n).Error
	return int(n), translate(err)
}
