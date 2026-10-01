package main

import "github.com/spf13/cobra"

const VERSION = "dev"

var rootCmd = &cobra.Command{
	Use:     "ivaldi",
	Short:   "Ivaldi CLI client",
	Long:    "The Ivaldi CLI client allows you to interact with the Ivaldi platform from the command line.",
	Version: VERSION,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {}
