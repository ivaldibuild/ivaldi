package database

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// Page sizes Paginate uses when a Query gives no limit, and at most.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// SortKey is a column a list can be sorted by. Only columns a Keyset names
// reach SQL, so a caller's sort field cannot inject SQL.
type SortKey[E any] struct {
	column string
	value  func(*E) any
	decode func(json.RawMessage) (any, error)
}

// TimeKey sorts by a timestamp column.
func TimeKey[E any](column string, get func(*E) time.Time) SortKey[E] {
	return SortKey[E]{
		column: column,
		value:  func(e *E) any { return get(e).UTC() },
		decode: func(raw json.RawMessage) (any, error) {
			var t time.Time
			err := json.Unmarshal(raw, &t)
			return t, err
		},
	}
}

// StringKey sorts by a text column.
func StringKey[E any](column string, get func(*E) string) SortKey[E] {
	return SortKey[E]{
		column: column,
		value:  func(e *E) any { return get(e) },
		decode: func(raw json.RawMessage) (any, error) {
			var s string
			err := json.Unmarshal(raw, &s)
			return s, err
		},
	}
}

// Keyset says how a list of rows of type E can be sorted and paged.
type Keyset[E any] struct {
	// Keys are the sort fields a Query may name, by the name it uses.
	Keys map[string]SortKey[E]
	// Default is the key used when a Query names none.
	Default string
	// IDColumn breaks ties between rows with equal sort values. It must be
	// unique within the list; empty means "id".
	IDColumn string
	// ID returns a row's IDColumn value.
	ID func(*E) string
}

// Page is one page of rows, before they are mapped to domain objects.
type Page[E any] struct {
	Rows        []E
	Total       int
	HasNext     bool
	HasPrevious bool
	Next        *string
	Previous    *string
}

// cursor marks a position in a sorted list: the sort it belongs to and a
// row's sort value and ID. It is opaque to callers.
type cursor struct {
	SortBy string          `json:"s"`
	Order  string          `json:"o"`
	Value  json.RawMessage `json:"v"`
	ID     string          `json:"id"`
}

func encodeCursor(c cursor) *string {
	b, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil {
		return cursor{}, invalid("the page cursor is not valid")
	}
	return c, nil
}

func invalid(msg string) error { return &pagination.InvalidQueryError{Message: msg} }

// Paginate reads one page of the rows base selects, sorted by q's key with
// the ID column breaking ties, using keyset (seek) paging so deep pages cost
// the same as the first. base must return a fresh query, with its model and
// filters, on each call. Invalid queries return an error matching
// pagination.ErrInvalidQuery before base is called.
func Paginate[E any](base func() *gorm.DB, q pagination.Query, ks Keyset[E]) (Page[E], error) {
	sortBy := q.SortBy
	if sortBy == "" {
		sortBy = ks.Default
	}
	key, ok := ks.Keys[sortBy]
	if !ok {
		return Page[E]{}, invalid(fmt.Sprintf("cannot sort by %q", q.SortBy))
	}
	order := q.SortOrder
	if order == "" {
		order = pagination.SortAscending
	}
	if order != pagination.SortAscending && order != pagination.SortDescending {
		return Page[E]{}, invalid(fmt.Sprintf("sort order %q must be asc or desc", q.SortOrder))
	}
	if q.NextCursor != nil && q.PreviousCursor != nil {
		return Page[E]{}, invalid("give a next or a previous cursor, not both")
	}
	backward := q.PreviousCursor != nil
	given := q.NextCursor
	if backward {
		given = q.PreviousCursor
	}
	var after *cursor
	var afterValue any
	if given != nil {
		c, err := decodeCursor(*given)
		if err != nil {
			return Page[E]{}, err
		}
		if c.SortBy != sortBy || c.Order != order {
			return Page[E]{}, invalid("the page cursor belongs to another sort")
		}
		if afterValue, err = key.decode(c.Value); err != nil {
			return Page[E]{}, invalid("the page cursor is not valid")
		}
		after = &c
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	limit = min(limit, MaxPageSize)
	idColumn := ks.IDColumn
	if idColumn == "" {
		idColumn = "id"
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return Page[E]{}, err
	}

	// Going forward in ascending order reads rows after the cursor; every
	// other combination flips the comparison and the scan direction.
	ascending := (order == pagination.SortAscending) != backward
	cmp, dir := ">", "ASC"
	if !ascending {
		cmp, dir = "<", "DESC"
	}
	query := base()
	if after != nil {
		query = query.Where(fmt.Sprintf("(%s, %s) %s (?, ?)", key.column, idColumn, cmp), afterValue, after.ID)
	}
	var rows []E
	err := query.Order(fmt.Sprintf("%s %s, %s %s", key.column, dir, idColumn, dir)).Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return Page[E]{}, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}

	p := Page[E]{Rows: rows, Total: int(total)}
	if backward {
		slices.Reverse(p.Rows)
		p.HasPrevious, p.HasNext = more, true
	} else {
		p.HasNext, p.HasPrevious = more, given != nil
	}
	at := func(e *E) *string {
		v, _ := json.Marshal(key.value(e))
		return encodeCursor(cursor{SortBy: sortBy, Order: order, Value: v, ID: ks.ID(e)})
	}
	if len(p.Rows) == 0 {
		// Rows were removed after the cursor was handed out. With nothing to
		// anchor a cursor on, report no neighbors; the caller starts over.
		p.HasNext, p.HasPrevious = false, false
		return p, nil
	}
	if p.HasNext {
		p.Next = at(&p.Rows[len(p.Rows)-1])
	}
	if p.HasPrevious {
		p.Previous = at(&p.Rows[0])
	}
	return p, nil
}

// ToResult maps a Page's rows, such as to domain objects, and returns them
// as a pagination.Result.
func ToResult[E, D any](p Page[E], convert func(*E) (D, error)) (*pagination.Result[D], error) {
	items := make([]D, 0, len(p.Rows))
	for i := range p.Rows {
		d, err := convert(&p.Rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return pagination.NewResult(items, p.Total, p.HasNext, p.HasPrevious, p.Next, p.Previous), nil
}
