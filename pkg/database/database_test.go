package database_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
)

func TestViolations(t *testing.T) {
	wrapped := func(code, constraint string) error {
		return fmt.Errorf("insert: %w", &pgconn.PgError{Code: code, ConstraintName: constraint})
	}

	tests := []struct {
		name   string
		check  func(error) (string, bool)
		err    error
		want   string
		wantOK bool
	}{
		{"unique", database.UniqueViolation, wrapped("23505", "idx_a"), "idx_a", true},
		{"unique, other code", database.UniqueViolation, wrapped("23503", "fk_a"), "", false},
		{"foreign key", database.ForeignKeyViolation, wrapped("23503", "fk_a"), "fk_a", true},
		{"check", database.CheckViolation, wrapped("23514", "chk_a"), "chk_a", true},
		{"not a PostgreSQL error", database.UniqueViolation, errors.New("boom"), "", false},
		{"nil", database.UniqueViolation, nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.check(tt.err)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithinTx(t *testing.T) {
	ctx := context.Background()
	db := databasetest.New(t)
	require.NoError(t, db.Exec("CREATE TABLE items (name text PRIMARY KEY)").Error)
	tx := database.NewTransactor(db)
	insert := func(ctx context.Context, name string) error {
		return database.Conn(ctx, db).Exec("INSERT INTO items (name) VALUES (?)", name).Error
	}
	names := func() []string {
		var out []string
		require.NoError(t, db.Raw("SELECT name FROM items ORDER BY name").Scan(&out).Error)
		return out
	}

	t.Run("commits when fn succeeds", func(t *testing.T) {
		require.NoError(t, tx.WithinTx(ctx, func(ctx context.Context) error { return insert(ctx, "a") }))
		assert.Equal(t, []string{"a"}, names())
	})

	t.Run("rolls back when fn fails", func(t *testing.T) {
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, insert(ctx, "b"))
			return errors.New("stop")
		})
		require.EqualError(t, err, "stop")
		assert.Equal(t, []string{"a"}, names())
	})

	t.Run("an inner call joins the outer transaction", func(t *testing.T) {
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, tx.WithinTx(ctx, func(ctx context.Context) error { return insert(ctx, "c") }))
			return errors.New("stop")
		})
		require.EqualError(t, err, "stop")
		assert.Equal(t, []string{"a"}, names(), "the inner write rolled back with the outer transaction")
	})

	t.Run("Conn without a transaction uses the database", func(t *testing.T) {
		require.NoError(t, insert(ctx, "d"))
		assert.Equal(t, []string{"a", "d"}, names())
	})

	t.Run("a unique violation is reported with its constraint", func(t *testing.T) {
		err := insert(ctx, "a")
		c, ok := database.UniqueViolation(err)
		assert.True(t, ok)
		assert.Equal(t, "items_pkey", c)
		assert.False(t, errors.Is(err, gorm.ErrRecordNotFound))
	})
}
