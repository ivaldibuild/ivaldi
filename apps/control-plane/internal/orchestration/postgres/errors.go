package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// translate maps PostgreSQL errors to orchestration errors.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return orchestration.ErrNotFound
	}
	if _, ok := database.UniqueViolation(err); ok {
		return fmt.Errorf("%w: %w", orchestration.ErrInvalid, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", orchestration.ErrInvalid, err)
	}
	return err
}
