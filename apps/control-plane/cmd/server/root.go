package server

import "github.com/spf13/cobra"

func NewServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Ivaldi Control Plane server",
	}
}
