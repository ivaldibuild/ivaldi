package postgres

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// teamKeyset sorts Teams by "created_at" (the default), "slug", or
// "display_name", with the ID breaking ties.
var teamKeyset = database.Keyset[teamEntity]{
	Keys: map[string]database.SortKey[teamEntity]{
		"created_at":   database.TimeKey("created_at", func(e *teamEntity) time.Time { return e.CreatedAt }),
		"slug":         database.StringKey("slug", func(e *teamEntity) string { return e.Slug }),
		"display_name": database.StringKey("display_name", func(e *teamEntity) string { return e.DisplayName }),
	},
	Default: "created_at",
	ID:      func(e *teamEntity) string { return e.ID },
}

// paginate is database.Paginate with its errors translated to team's.
func paginate[E any](base func() *gorm.DB, q pagination.Query, ks database.Keyset[E]) (database.Page[E], error) {
	p, err := database.Paginate(base, q, ks)
	if err == nil {
		return p, nil
	}
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return p, team.NewErrInvalidInput(err.Error())
	}
	return p, translate(err)
}
