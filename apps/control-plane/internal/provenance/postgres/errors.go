package postgres

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return provenance.ErrNotFound
	}
	if c, ok := database.UniqueViolation(err); ok {
		switch c {
		case "change_commits_pkey":
			return fmt.Errorf("%w: %w", provenance.ErrAlreadyExists, err)
		default:
			return fmt.Errorf("%w: %w", provenance.ErrConflict, err)
		}
	}
	if _, ok := database.ForeignKeyViolation(err); ok {
		return fmt.Errorf("%w: %w", provenance.ErrNotFound, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", provenance.ErrInvalid, err)
	}
	return err
}
