package database

import (
	"database/sql/driver"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
)

// TextArray is a TEXT[] column, for GORM entities. database/sql cannot scan an array, so Scan
// goes through pgx's type map; Value encodes it the same way, as PostgreSQL
// array text.
type TextArray []string

// typeMaps lends pgx type maps to scans: a map is not safe for concurrent use.
var typeMaps = sync.Pool{New: func() any { return pgtype.NewMap() }}

func (a *TextArray) Scan(src any) error {
	m := typeMaps.Get().(*pgtype.Map)
	defer typeMaps.Put(m)
	var v []string
	if err := m.SQLScanner(&v).Scan(src); err != nil {
		return fmt.Errorf("scan text array: %w", err)
	}
	*a = v
	return nil
}

func (a TextArray) Value() (driver.Value, error) {
	m := typeMaps.Get().(*pgtype.Map)
	defer typeMaps.Put(m)
	v := []string(a)
	if v == nil {
		v = []string{}
	}
	b, err := m.Encode(pgtype.TextArrayOID, pgtype.TextFormatCode, v, nil)
	if err != nil {
		return nil, fmt.Errorf("encode text array: %w", err)
	}
	return string(b), nil
}
