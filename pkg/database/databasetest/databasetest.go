// Package databasetest gives repository tests a throwaway PostgreSQL database.
//
// Tests run only when IVALDI_TEST_DATABASE_URL names a server whose user may
// create databases; otherwise they skip, so `go test ./...` works without
// PostgreSQL.
package databasetest

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// URLEnv names the environment variable holding the PostgreSQL server URL.
const URLEnv = "IVALDI_TEST_DATABASE_URL"

// New creates an empty database, applies the *.up.sql files of each
// migrations file system in name order, and drops the database when the test
// ends. It skips the test when URLEnv is unset.
//
// Each file system is applied in turn, so a test can apply several
// capabilities' migrations. Migration files are applied directly, without
// golang-migrate's version table, because the database is thrown away.
func New(t testing.TB, migrations ...fs.FS) *gorm.DB {
	t.Helper()
	serverURL := os.Getenv(URLEnv)
	if serverURL == "" {
		t.Skipf("set %s to run PostgreSQL tests", URLEnv)
	}

	admin, err := database.Open(serverURL)
	if err != nil {
		t.Fatalf("connect to %s: %v", URLEnv, err)
	}
	name := "ivaldi_test_" + strings.ToLower(ulid.Make().String())
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		closeDB(admin)
	})

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse %s: %v", URLEnv, err)
	}
	u.Path = "/" + name
	db, err := database.Open(u.String())
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(func() { closeDB(db) })

	for _, m := range migrations {
		if err := apply(db, m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func apply(db *gorm.DB, migrations fs.FS) error {
	files, err := fs.Glob(migrations, "*.up.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	slices.Sort(files)
	for _, f := range files {
		sql, err := fs.ReadFile(migrations, f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			return fmt.Errorf("apply migration %s: %w", f, err)
		}
	}
	return nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
