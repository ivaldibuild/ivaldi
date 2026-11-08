package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// translate maps PostgreSQL errors to the capability's errors by constraint
// name.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return versioncontrol.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		switch c {
		case "idx_sources_repository":
			return fmt.Errorf("%w: %w", versioncontrol.ErrAlreadyExists, err)
		case "idx_connections_webhook_route":
			return fmt.Errorf("%w: %w", versioncontrol.ErrAccountConnected, err)
		default:
			return fmt.Errorf("%w: %w", versioncontrol.ErrConflict, err)
		}
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", versioncontrol.ErrNotFound, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", versioncontrol.ErrInvalid, err)
	}
	return err
}

// updated turns an update that matched no row into ErrNotFound.
func updated(res *gorm.DB) error {
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return versioncontrol.ErrNotFound
	}
	return nil
}
