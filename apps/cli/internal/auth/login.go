package auth

import "github.com/spf13/cobra"

func NewLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Ivaldi control plane",
		RunE:  runLogin,
	}

	return cmd
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Implement the login logic here
	return nil
}
