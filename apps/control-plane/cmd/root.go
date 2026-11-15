package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-control-plane/cmd/gateway"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/indexer"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/migrate"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/server"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/worker"
)

// rootCmd is the ivaldi binary. Each subcommand starts one component, so a
// deployment runs the same image with a different command (ADR 0010).
var rootCmd = &cobra.Command{
	Use:     "ivaldi",
	Short:   "Run an Ivaldi component: the control plane, gateway, worker, or indexer",
	Version: displayVersion(),
}

// displayVersion is what `ivaldi version` prints: the release version, and
// the commit it was built from when the build stamped one. Programmatic
// readers of the version, such as the API's description, use VERSION alone.
func displayVersion() string {
	if COMMIT == "" || COMMIT == "unknown" {
		return VERSION
	}
	return fmt.Sprintf("%s (%s, %s)", VERSION, COMMIT, BUILDTIME)
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(
		server.NewServeCommand(VERSION),
		server.NewOpenAPICommand(),
		migrate.NewMigrateCommand(),
		gateway.NewGatewayCommand(),
		indexer.NewIndexerCommand(),
		worker.NewWorkerCommand(),
	)

	// No default file: a component reads its settings from the environment,
	// and from this file when one is given. Environment variables win.
	rootCmd.PersistentFlags().StringP("config", "c", "", "YAML config file; environment variables override it")
}
