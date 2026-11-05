package main

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-control-plane/cmd/gateway"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/indexer"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/server"
	"github.com/ivaldibuild/ivaldi-control-plane/cmd/worker"
)

// rootCmd is the ivaldi binary. Each subcommand starts one component, so a
// deployment runs the same image with a different command (ADR 0010).
var rootCmd = &cobra.Command{
	Use:     "ivaldi",
	Short:   "Run an Ivaldi component: the control plane, gateway, worker, or indexer",
	Version: VERSION,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(
		server.NewServeCommand(VERSION),
		gateway.NewGatewayCommand(),
		indexer.NewIndexerCommand(),
		worker.NewWorkerCommand(),
	)

	// No default file: a component reads its settings from the environment,
	// and from this file when one is given. Environment variables win.
	rootCmd.PersistentFlags().StringP("config", "c", "", "YAML config file; environment variables override it")
}
