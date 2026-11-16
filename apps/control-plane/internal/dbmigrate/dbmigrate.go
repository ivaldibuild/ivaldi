// Package dbmigrate applies the capabilities' schema migrations from the
// copies embedded in the binary, so a released image upgrades its own database
// without a source checkout and without the golang-migrate CLI.
//
// The Makefile still uses golang-migrate for authoring (migration_generate)
// and CI runs both paths, so the embedded copy cannot drift from the files.
package dbmigrate

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver for database/sql

	auditmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	credentialsmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/migrations"
	deliverymigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/migrations"
	iammigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	orchestrationmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration/migrations"
	outboxmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	policymigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/policy/migrations"
	provenancemigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/provenance/migrations"
	teammigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/team/migrations"
	versioncontrolmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/migrations"
	workspacemigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/migrations"
)

// Capability is one capability's migrations.
type Capability struct {
	// Name is the capability's directory name, which the Makefile and CI use.
	Name string
	// FS holds the capability's .sql files.
	FS fs.FS
}

// Capabilities returns every capability's migrations, in the order they are
// applied. There are no foreign keys between capabilities, so the order is for
// readability only, and it matches the Makefile's CAPABILITIES.
func Capabilities() []Capability {
	return []Capability{
		{Name: "outbox", FS: outboxmigrations.FS},
		{Name: "audit", FS: auditmigrations.FS},
		{Name: "iam", FS: iammigrations.FS},
		{Name: "workspace", FS: workspacemigrations.FS},
		{Name: "versioncontrol", FS: versioncontrolmigrations.FS},
		{Name: "delivery", FS: deliverymigrations.FS},
		{Name: "provenance", FS: provenancemigrations.FS},
		{Name: "team", FS: teammigrations.FS},
		{Name: "policy", FS: policymigrations.FS},
		{Name: "orchestration", FS: orchestrationmigrations.FS},
		{Name: "credentials", FS: credentialsmigrations.FS},
	}
}

// VersionTable is the table that tracks a capability's migration version.
// Every capability numbers its migrations from 000001, so they cannot share
// one. It matches the Makefile's migrations_table, including versioncontrol's
// version_control_schema_migrations.
func VersionTable(name string) string {
	if name == "versioncontrol" {
		return "version_control_schema_migrations"
	}
	return name + "_schema_migrations"
}

// Up applies every pending migration, or only the named capability's.
func Up(databaseURL, capability string) error {
	caps, err := selectCapabilities(capability)
	if err != nil {
		return err
	}
	for _, c := range caps {
		err := withMigrator(databaseURL, c, func(m *migrate.Migrate) error {
			if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return nil
}

// Down rolls one capability back by steps migrations. A capability is named
// because rolling every capability back at once is never what an upgrade
// wants.
func Down(databaseURL, capability string, steps int) error {
	if steps < 1 {
		return errors.New("migrate: steps must be at least 1")
	}
	caps, err := selectCapabilities(capability)
	if err != nil {
		return err
	}
	for _, c := range caps {
		err := withMigrator(databaseURL, c, func(m *migrate.Migrate) error {
			if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return nil
}

// State is a capability's migration version. Dirty means a migration failed
// part way and the schema must be repaired before anything else is applied.
type State struct {
	Version uint
	Dirty   bool
}

// Version is each capability's current migration state, with version 0 for
// one that has never been migrated. A dirty capability is reported, not
// returned as an error, so the caller still sees every other capability's.
func Version(databaseURL string) (map[string]State, error) {
	out := make(map[string]State, len(Capabilities()))
	for _, c := range Capabilities() {
		var state State
		err := withMigrator(databaseURL, c, func(m *migrate.Migrate) error {
			v, dirty, err := m.Version()
			if errors.Is(err, migrate.ErrNilVersion) {
				return nil
			}
			if err != nil {
				return err
			}
			state = State{Version: v, Dirty: dirty}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		out[c.Name] = state
	}
	return out, nil
}

// selectCapabilities resolves the --capability value: empty means every
// capability, and an unknown name is an error rather than a silent no-op.
func selectCapabilities(capability string) ([]Capability, error) {
	all := Capabilities()
	if capability == "" {
		return all, nil
	}
	for _, c := range all {
		if c.Name == capability {
			return []Capability{c}, nil
		}
	}
	return nil, unknownCapability(capability, all)
}

func unknownCapability(capability string, all []Capability) error {
	names := make([]string, 0, len(all))
	for _, c := range all {
		names = append(names, c.Name)
	}
	return fmt.Errorf("migrate: unknown capability %q, one of %v", capability, names)
}

// withMigrator opens its own connection per capability: golang-migrate holds a
// lock on the connection, and one capability's migration table cannot be
// swapped for another's on a shared one.
//
// The driver pins a connection from db for its lifetime, which db.Close alone
// does not release, so every path closes the driver or the migrator too.
// Closing db again after them is a no-op.
func withMigrator(databaseURL string, c Capability, fn func(*migrate.Migrate) error) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open the database: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(2)

	driver, err := postgres.WithInstance(db, &postgres.Config{MigrationsTable: VersionTable(c.Name)})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	source, err := iofs.New(c.FS, ".")
	if err != nil {
		_ = driver.Close()
		return fmt.Errorf("read the embedded migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		_ = source.Close()
		_ = driver.Close()
		return fmt.Errorf("build the migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	return fn(m)
}
