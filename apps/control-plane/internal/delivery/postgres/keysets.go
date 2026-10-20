package postgres

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// standardKeyset sorts records with a slug and a display name by
// "created_at" (the default), "slug", or "display_name", with the ID
// breaking ties.
func standardKeyset[E any](id func(*E) string, createdAt func(*E) time.Time, slug, displayName func(*E) string) database.Keyset[E] {
	return database.Keyset[E]{
		Keys: map[string]database.SortKey[E]{
			"created_at":   database.TimeKey("created_at", createdAt),
			"slug":         database.StringKey("slug", slug),
			"display_name": database.StringKey("display_name", displayName),
		},
		Default: "created_at",
		ID:      id,
	}
}

var (
	environmentKeyset = standardKeyset(
		func(e *environmentEntity) string { return e.ID },
		func(e *environmentEntity) time.Time { return e.CreatedAt },
		func(e *environmentEntity) string { return e.Slug },
		func(e *environmentEntity) string { return e.DisplayName },
	)
	lifecycleKeyset = standardKeyset(
		func(e *lifecycleEntity) string { return e.ID },
		func(e *lifecycleEntity) time.Time { return e.CreatedAt },
		func(e *lifecycleEntity) string { return e.Slug },
		func(e *lifecycleEntity) string { return e.DisplayName },
	)
	targetKeyset = standardKeyset(
		func(e *targetEntity) string { return e.ID },
		func(e *targetEntity) time.Time { return e.CreatedAt },
		func(e *targetEntity) string { return e.Slug },
		func(e *targetEntity) string { return e.DisplayName },
	)
	registryKeyset = standardKeyset(
		func(e *registryEntity) string { return e.ID },
		func(e *registryEntity) time.Time { return e.CreatedAt },
		func(e *registryEntity) string { return e.Slug },
		func(e *registryEntity) string { return e.DisplayName },
	)
	applicationKeyset = standardKeyset(
		func(e *applicationEntity) string { return e.ID },
		func(e *applicationEntity) time.Time { return e.CreatedAt },
		func(e *applicationEntity) string { return e.Slug },
		func(e *applicationEntity) string { return e.DisplayName },
	)
)

// paginate is database.Paginate with its errors translated to delivery's.
func paginate[E any](base func() *gorm.DB, q pagination.Query, ks database.Keyset[E]) (database.Page[E], error) {
	p, err := database.Paginate(base, q, ks)
	if err == nil {
		return p, nil
	}
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return p, delivery.NewErrInvalidInput(err.Error())
	}
	return p, translate(err)
}
