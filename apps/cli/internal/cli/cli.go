package cli

import (
	"github.com/ivaldibuild/ivaldi-cli/internal/auth"
	"github.com/ivaldibuild/ivaldi-cli/internal/helpers"
	"github.com/ivaldibuild/ivaldi-cli/internal/types"
	"github.com/spf13/cobra"
)

const CLI_VERSION = "dev"

func NewCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ivaldictl",
		Aliases:      []string{"iv", "ivldctl"},
		Short:        "Ivaldi CLI",
		Long:         "Ivaldi CLI controls the Ivaldi control plane",
		Version:      CLI_VERSION,
		SilenceUsage: true,
	}

	helpers.AddPersistentFlags(cmd, initPersistentFlags())

	// Register sub-commands
	cmd.AddCommand(auth.NewCommand())

	return cmd
}

func initPersistentFlags() map[string]types.Flag {
	return map[string]types.Flag{
		"server-url": {
			Type:        "string",
			Short:       "s",
			Description: "URL of the Ivaldi control plane server",
			Default:     "http://localhost:8080",
		},
		"verbose": {
			Type:        "bool",
			Short:       "V",
			Description: "Enable verbose output",
			Default:     false,
		},
		"output": {
			Type:        "string",
			Short:       "o",
			Description: "Output format (e.g., json, yaml)",
			Default:     "json",
		},
	}
}
