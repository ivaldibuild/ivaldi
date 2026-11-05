package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// translate maps PostgreSQL errors to iam errors. A unique violation is a
// slug, email, or membership another record already holds.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return iam.ErrNotFound
	}
	if _, ok := database.UniqueViolation(err); ok {
		return fmt.Errorf("%w: %w", iam.ErrAlreadyExists, err)
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", iam.ErrNotFound, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", iam.ErrInvalid, err)
	}
	return err
}
