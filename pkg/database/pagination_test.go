package database_test

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type pageRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

func (pageRow) TableName() string { return "page_rows" }

var pageKeyset = database.Keyset[pageRow]{
	Keys: map[string]database.SortKey[pageRow]{
		"created_at": database.TimeKey("created_at", func(r *pageRow) time.Time { return r.CreatedAt }),
		"name":       database.StringKey("name", func(r *pageRow) string { return r.Name }),
	},
	Default: "created_at",
	ID:      func(r *pageRow) string { return r.ID },
}

func TestPaginateRejectsInvalidQueries(t *testing.T) {
	notCalled := func() *gorm.DB {
		t.Fatal("an invalid query must not reach the database")
		return nil
	}
	cursor := nameCursor()
	tests := map[string]pagination.Query{
		"unknown sort":     {SortBy: "password"},
		"unknown order":    {SortOrder: "sideways"},
		"both cursors":     {NextCursor: new("a"), PreviousCursor: new("b")},
		"garbage cursor":   {NextCursor: new("%%%")},
		"base64 non-JSON":  {NextCursor: new("bm90IGpzb24")},
		"bad cursor value": {NextCursor: new(base64.RawURLEncoding.EncodeToString([]byte(`{"s":"created_at","o":"asc","v":"yesterday","id":"r00"}`)))},
		"other sort":       {NextCursor: cursor},
		"other order":      {SortBy: "name", SortOrder: pagination.SortDescending, NextCursor: cursor},
	}
	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := database.Paginate(notCalled, q, pageKeyset)
			assert.ErrorIs(t, err, pagination.ErrInvalidQuery)
		})
	}
}

// nameCursor is a cursor for an ascending sort by name, in the format
// Paginate writes, so validation is tested without a database.
func nameCursor() *string {
	return new(base64.RawURLEncoding.EncodeToString([]byte(`{"s":"name","o":"asc","v":"n01","id":"r00"}`)))
}

// seedPageRows inserts n rows, r00 … r(n-1), each a minute after the last,
// with names in the reverse order of their creation.
func seedPageRows(t *testing.T, db *gorm.DB, n int) []pageRow {
	t.Helper()
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS page_rows (id text PRIMARY KEY, name text NOT NULL, created_at timestamptz NOT NULL)").Error)
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	rows := make([]pageRow, n)
	for i := range rows {
		rows[i] = pageRow{ID: fmt.Sprintf("r%02d", i), Name: fmt.Sprintf("n%02d", n-i), CreatedAt: start.Add(time.Duration(i) * time.Minute)}
	}
	require.NoError(t, db.Create(&rows).Error)
	return rows
}

func ids(rows []pageRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestPaginateWalksForwardAndBack(t *testing.T) {
	db := databasetest.New(t)
	seedPageRows(t, db, 7)
	base := func() *gorm.DB { return db.Model(&pageRow{}) }
	q := pagination.Query{Limit: 3}

	first, err := database.Paginate(base, q, pageKeyset)
	require.NoError(t, err)
	assert.Equal(t, []string{"r00", "r01", "r02"}, ids(first.Rows))
	assert.Equal(t, 7, first.Total)
	assert.True(t, first.HasNext)
	assert.False(t, first.HasPrevious)
	assert.Nil(t, first.Previous)

	q.NextCursor = first.Next
	second, err := database.Paginate(base, q, pageKeyset)
	require.NoError(t, err)
	assert.Equal(t, []string{"r03", "r04", "r05"}, ids(second.Rows))
	assert.True(t, second.HasNext)
	assert.True(t, second.HasPrevious)

	q.NextCursor = second.Next
	last, err := database.Paginate(base, q, pageKeyset)
	require.NoError(t, err)
	assert.Equal(t, []string{"r06"}, ids(last.Rows))
	assert.False(t, last.HasNext)
	assert.Nil(t, last.Next)
	assert.True(t, last.HasPrevious)

	q.NextCursor, q.PreviousCursor = nil, last.Previous
	back, err := database.Paginate(base, q, pageKeyset)
	require.NoError(t, err)
	assert.Equal(t, ids(second.Rows), ids(back.Rows), "going back returns the same page, in order")
	assert.True(t, back.HasNext)
	assert.True(t, back.HasPrevious)

	q.PreviousCursor = back.Previous
	front, err := database.Paginate(base, q, pageKeyset)
	require.NoError(t, err)
	assert.Equal(t, ids(first.Rows), ids(front.Rows))
	assert.False(t, front.HasPrevious, "the first page has nothing before it")
	assert.Nil(t, front.Previous)
}

func TestPaginateSortsAndFilters(t *testing.T) {
	db := databasetest.New(t)
	seedPageRows(t, db, 5)
	base := func() *gorm.DB { return db.Model(&pageRow{}).Where("id <> ?", "r02") }

	t.Run("descending", func(t *testing.T) {
		p, err := database.Paginate(base, pagination.Query{SortOrder: pagination.SortDescending, Limit: 2}, pageKeyset)
		require.NoError(t, err)
		assert.Equal(t, []string{"r04", "r03"}, ids(p.Rows))
		assert.Equal(t, 4, p.Total, "the total counts only what base selects")

		next, err := database.Paginate(base, pagination.Query{SortOrder: pagination.SortDescending, Limit: 2, NextCursor: p.Next}, pageKeyset)
		require.NoError(t, err)
		assert.Equal(t, []string{"r01", "r00"}, ids(next.Rows))
		assert.False(t, next.HasNext)
	})

	t.Run("by another key", func(t *testing.T) {
		p, err := database.Paginate(base, pagination.Query{SortBy: "name"}, pageKeyset)
		require.NoError(t, err)
		assert.Equal(t, []string{"r04", "r03", "r01", "r00"}, ids(p.Rows), "names run opposite to creation")
		assert.False(t, p.HasNext)
		assert.Nil(t, p.Next)
	})

	t.Run("limits", func(t *testing.T) {
		p, err := database.Paginate(base, pagination.Query{}, pageKeyset)
		require.NoError(t, err)
		assert.Len(t, p.Rows, 4, "no limit means the default page size")

		p, err = database.Paginate(base, pagination.Query{Limit: database.MaxPageSize + 1}, pageKeyset)
		require.NoError(t, err)
		assert.Len(t, p.Rows, 4)
	})
}

func TestPaginateBreaksTiesByID(t *testing.T) {
	db := databasetest.New(t)
	require.NoError(t, db.Exec("CREATE TABLE page_rows (id text PRIMARY KEY, name text NOT NULL, created_at timestamptz NOT NULL)").Error)
	same := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{"c", "a", "b", "d"} {
		require.NoError(t, db.Create(&pageRow{ID: id, Name: "x", CreatedAt: same}).Error)
	}
	base := func() *gorm.DB { return db.Model(&pageRow{}) }

	var seen []string
	q := pagination.Query{Limit: 1}
	for {
		p, err := database.Paginate(base, q, pageKeyset)
		require.NoError(t, err)
		seen = append(seen, ids(p.Rows)...)
		if !p.HasNext {
			break
		}
		q.NextCursor = p.Next
	}
	assert.Equal(t, []string{"a", "b", "c", "d"}, seen, "rows with equal sort values are each visited once")
}

func TestToResult(t *testing.T) {
	p := database.Page[pageRow]{Rows: []pageRow{{ID: "a"}, {ID: "b"}}, Total: 9, HasNext: true, Next: new("n")}
	r, err := database.ToResult(p, func(r *pageRow) (string, error) { return "row " + r.ID, nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"row a", "row b"}, r.Items)
	assert.Equal(t, 9, r.TotalCount)
	assert.True(t, r.HasNext)
	assert.Equal(t, "n", *r.NextCursor)

	_, err = database.ToResult(p, func(*pageRow) (string, error) { return "", fmt.Errorf("bad row") })
	assert.EqualError(t, err, "bad row")
}

func TestPaginateEmptyPageHasNoNeighbors(t *testing.T) {
	db := databasetest.New(t)
	seedPageRows(t, db, 4)
	base := func() *gorm.DB { return db.Model(&pageRow{}) }

	first, err := database.Paginate(base, pagination.Query{Limit: 2}, pageKeyset)
	require.NoError(t, err)
	// The rest of the list goes away while the caller holds the cursor.
	require.NoError(t, db.Exec("DELETE FROM page_rows WHERE id IN ('r02', 'r03')").Error)

	empty, err := database.Paginate(base, pagination.Query{Limit: 2, NextCursor: first.Next}, pageKeyset)
	require.NoError(t, err)
	assert.Empty(t, empty.Rows)
	assert.False(t, empty.HasNext)
	assert.False(t, empty.HasPrevious, "there is no row to anchor a previous cursor on")
	assert.Nil(t, empty.Next)
	assert.Nil(t, empty.Previous)
}
