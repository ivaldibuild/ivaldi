package gateway

import "github.com/spf13/cobra"

func NewGatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start the Ivaldi Gateway",
	}
}
