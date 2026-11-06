package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// slugIndex is the partial unique index that keeps active Teams' slugs
// unique in their Organization.
const slugIndex = "idx_teams_organization_slug"

// translate maps PostgreSQL errors to team errors by constraint name.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return team.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		if c == slugIndex {
			return fmt.Errorf("%w: %w", team.ErrSlugTaken, err)
		}
		return fmt.Errorf("%w: %w", team.ErrConflict, err)
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", team.ErrNotFound, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", team.ErrInvalid, err)
	}
	return err
}

// updated turns an update that matched no row into ErrNotFound.
func updated(res *gorm.DB) error {
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return team.ErrNotFound
	}
	return nil
}
