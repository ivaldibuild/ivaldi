package main

import "github.com/spf13/cobra"

// VERSION is set at build time with -ldflags "-X main.VERSION=<version>".
var VERSION = "dev"

var rootCmd = &cobra.Command{
	Use:     "ivaldictl",
	Aliases: []string{"iv", "ivld"},
	Short:   "Ivaldi CLI client",
	Long:    "The Ivaldi CLI client allows you to interact with the Ivaldi platform from the command line.",
	Version: VERSION,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringP("server", "s", "", "Remote server the cli client should connect to")
}
