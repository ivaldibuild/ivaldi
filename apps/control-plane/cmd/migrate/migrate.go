// Package migrate is the `ivaldi migrate` command: it applies the migrations
// the binary carries, so a released image upgrades its own database.
package migrate

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/dbmigrate"
	"github.com/ivaldibuild/ivaldi/pkg/env"
)

// NewMigrateCommand returns the migrate command and its subcommands.
func NewMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, or inspect the control plane's database schema",
		Long: "Apply the migrations this build carries. Run `migrate up` before " +
			"starting a new control plane: schema changes are not applied on start.",
		// Errors come from the database, not from how the command was called.
		SilenceUsage: true,
	}
	cmd.AddCommand(newUpCommand(), newDownCommand(), newVersionCommand())
	return cmd
}

func newUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "up",
		Short:        "Apply every pending migration, or one capability's",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			capability, err := cmd.Flags().GetString("capability")
			if err != nil {
				return err
			}
			url, err := databaseURL(cmd)
			if err != nil {
				return err
			}
			return dbmigrate.Up(url, capability)
		},
	}
	cmd.Flags().String("capability", "", "apply only this capability's migrations")
	return cmd
}

func newDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "down",
		Short:        "Roll a capability back",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			capability, err := cmd.Flags().GetString("capability")
			if err != nil {
				return err
			}
			if capability == "" {
				return errors.New("migrate down: --capability is required; rolling every capability back at once is not an upgrade")
			}
			steps, err := cmd.Flags().GetInt("steps")
			if err != nil {
				return err
			}
			url, err := databaseURL(cmd)
			if err != nil {
				return err
			}
			return dbmigrate.Down(url, capability, steps)
		},
	}
	cmd.Flags().String("capability", "", "the capability to roll back")
	cmd.Flags().Int("steps", 1, "how many migrations to roll back")
	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print each capability's current migration version",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := databaseURL(cmd)
			if err != nil {
				return err
			}
			states, err := dbmigrate.Version(url)
			if err != nil {
				return err
			}
			// Every capability is printed, dirty ones marked, so the one to
			// repair is visible beside the rest; then the command fails.
			var dirty []string
			for _, c := range dbmigrate.Capabilities() {
				s := states[c.Name]
				line := fmt.Sprintf("%-16s %d", c.Name, s.Version)
				if s.Dirty {
					line += " dirty"
					dirty = append(dirty, c.Name)
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
					return err
				}
			}
			if len(dirty) > 0 {
				return fmt.Errorf("dirty: %v: a migration failed part way; repair the schema, then force the version with golang-migrate", dirty)
			}
			return nil
		},
	}
}

// databaseURL reads the address from the same config the components use:
// --config, then IVALDI_DATABASE_URL, then persistence.database_url.
func databaseURL(cmd *cobra.Command) (string, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", err
	}
	var cfg config.Config
	if err := env.Load(&cfg, env.Options{YAMLPath: path}); err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	if cfg.Persistence.DatabaseURL == nil || *cfg.Persistence.DatabaseURL == "" {
		return "", errors.New("no database: set IVALDI_DATABASE_URL or persistence.database_url, or pass --config")
	}
	return *cfg.Persistence.DatabaseURL, nil
}
