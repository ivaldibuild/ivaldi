package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// slugIndexes are the partial unique indexes that keep active slugs, and
// artifact names, unique in their scope.
var slugIndexes = map[string]bool{
	"idx_environments_workspace_slug": true,
	"idx_lifecycles_workspace_slug":   true,
	"idx_targets_environment_slug":    true,
	"idx_clusters_slug":               true,
	"idx_registries_workspace_slug":   true,
	"idx_applications_project_slug":   true,
	"idx_application_artifacts_name":  true,
	"idx_channels_application_slug":   true,
	"idx_variables_project_name":      true,
	"idx_variables_application_name":  true,
}

// translate maps PostgreSQL errors to delivery errors by constraint name.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return delivery.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		switch {
		case slugIndexes[c]:
			return fmt.Errorf("%w: %w", delivery.ErrSlugTaken, err)
		case c == "application_targets_pkey" || c == "idx_application_targets_organization",
			c == "idx_releases_version", c == "idx_builds_digest", c == "idx_deployments_organization":
			return fmt.Errorf("%w: %w", delivery.ErrAlreadyExists, err)
		case c == "idx_deployments_in_flight":
			return fmt.Errorf("%w: %w", delivery.ErrDeploymentInFlight, err)
		case c == "idx_targets_cluster_namespace":
			return fmt.Errorf("%w: %w", delivery.ErrNamespaceTaken, err)
		default:
			return fmt.Errorf("%w: %w", delivery.ErrConflict, err)
		}
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", delivery.ErrNotFound, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", delivery.ErrInvalid, err)
	}
	return err
}

// updated turns an update that matched no row into ErrNotFound.
func updated(res *gorm.DB) error {
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return delivery.ErrNotFound
	}
	return nil
}

// upsertOwned inserts child rows, or updates columns of the ones that exist,
// but only rows that already belong to the same parent and Organization. A
// row whose ID belongs to another parent is neither inserted nor updated,
// which is reported as ErrConflict instead of overwriting it.
func upsertOwned(tx *gorm.DB, rows any, n int, table, parentColumn string, columns []string) error {
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns(columns),
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: fmt.Sprintf(
			"%[1]s.organization_id = excluded.organization_id AND %[1]s.%[2]s = excluded.%[2]s", table, parentColumn)}}},
	}).Create(rows)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != int64(n) {
		return fmt.Errorf("%w: a %s ID belongs to another record", delivery.ErrConflict, table)
	}
	return nil
}
