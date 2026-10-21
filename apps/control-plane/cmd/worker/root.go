package worker

import "github.com/spf13/cobra"

func NewWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Start the Ivaldi worker",
	}
}
