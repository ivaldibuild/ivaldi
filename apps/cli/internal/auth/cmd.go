package auth

import (
	"github.com/ivaldibuild/ivaldi-cli/internal/helpers"
	"github.com/ivaldibuild/ivaldi-cli/internal/types"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Perform authentication operations on the Ivaldi control plane",
	}

	helpers.AddPersistentFlags(cmd, initPersistentFlags())

	cmd.AddCommand(NewLoginCommand())

	return cmd
}

func initPersistentFlags() map[string]types.Flag {
	return map[string]types.Flag{
		"api-token": {
			Type:        "string",
			Short:       "a",
			Description: "API token for authentication. Use when you dont want to use the browser-based login",
			Default:     "",
		},
	}
}
