package postgres

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

var changeKeyset = database.Keyset[changeEntity]{
	Keys: map[string]database.SortKey[changeEntity]{
		"created_at": database.TimeKey("created_at", func(e *changeEntity) time.Time { return e.CreatedAt }),
		"title":      database.StringKey("title", func(e *changeEntity) string { return e.Title }),
	},
	Default: "created_at",
	ID:      func(e *changeEntity) string { return e.ID },
}

func paginate(base func() *gorm.DB, q pagination.Query) (database.Page[changeEntity], error) {
	p, err := database.Paginate(base, q, changeKeyset)
	if err == nil {
		return p, nil
	}
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return p, provenance.NewErrInvalidInput(err.Error())
	}
	return p, translate(err)
}
