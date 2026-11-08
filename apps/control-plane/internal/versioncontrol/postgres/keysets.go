package postgres

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// sourceKeyset sorts Sources by "created_at" (the default) or "full_name",
// with the ID breaking ties.
var sourceKeyset = database.Keyset[sourceEntity]{
	Keys: map[string]database.SortKey[sourceEntity]{
		"created_at": database.TimeKey("created_at", func(e *sourceEntity) time.Time { return e.CreatedAt }),
		"full_name":  database.StringKey("full_name", func(e *sourceEntity) string { return e.FullName }),
	},
	Default: "created_at",
	ID:      func(e *sourceEntity) string { return e.ID },
}

// paginate is database.Paginate with its errors translated.
func paginate(base func() *gorm.DB, q pagination.Query) (database.Page[sourceEntity], error) {
	p, err := database.Paginate(base, q, sourceKeyset)
	if err == nil {
		return p, nil
	}
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return p, versioncontrol.NewErrInvalidInput(err.Error())
	}
	return p, translate(err)
}
