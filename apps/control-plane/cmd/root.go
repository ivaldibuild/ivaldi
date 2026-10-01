package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:     "serve",
	Short:   "Start the Ivaldi Control Plane",
	Version: VERSION,
}
