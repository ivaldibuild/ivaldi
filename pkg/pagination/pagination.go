// Package pagination holds the types a paged list is asked for and returned
// with. It has no database code, so domain packages can import it;
// pkg/database implements cursor paging with them.
package pagination

import "errors"

// Sort orders a Query accepts.
const (
	SortAscending  = "asc"
	SortDescending = "desc"
)

// ErrInvalidQuery means a Query asked for an unknown sort, an unknown order,
// both cursors, or a cursor that is malformed or belongs to another sort.
var ErrInvalidQuery = errors.New("invalid page query")

// InvalidQueryError says what is wrong with a Query. It matches ErrInvalidQuery.
type InvalidQueryError struct {
	Message string
}

func (e *InvalidQueryError) Error() string { return e.Message }

func (e *InvalidQueryError) Is(target error) bool { return target == ErrInvalidQuery }

// Query asks for one page of a list. Lists page by cursor, so a page stays
// stable while records are added.
type Query struct {
	// Limit is the maximum number of items to return. Zero means the
	// implementation's default; implementations cap it.
	Limit int
	// SortBy names the field to sort by. Each list documents the fields it
	// accepts; empty means its default.
	SortBy string
	// SortOrder is SortAscending (the default) or SortDescending.
	SortOrder string
	// NextCursor asks for the page after the one that returned it.
	NextCursor *string
	// PreviousCursor asks for the page before the one that returned it. At
	// most one of NextCursor and PreviousCursor may be set.
	PreviousCursor *string
}

// Result is one page of items with what is needed to move to the pages
// around it.
type Result[T any] struct {
	Items []T `json:"items"`
	// TotalCount is the number of items in the whole list.
	TotalCount int  `json:"total_count"`
	HasNext    bool `json:"has_next"`
	// HasPrevious is true when the page was reached through a cursor.
	HasPrevious bool `json:"has_previous"`
	// NextCursor and PreviousCursor are set when there is a page that way.
	NextCursor     *string `json:"next_cursor"`
	PreviousCursor *string `json:"previous_cursor"`
}

// NewResult returns a Result with the given items, total count, pagination flags, and cursors.
func NewResult[T any](items []T, totalCount int, hasNext, hasPrevious bool, nextCursor, previousCursor *string) *Result[T] {
	return &Result[T]{
		Items:          items,
		TotalCount:     totalCount,
		HasNext:        hasNext,
		HasPrevious:    hasPrevious,
		NextCursor:     nextCursor,
		PreviousCursor: previousCursor,
	}
}
