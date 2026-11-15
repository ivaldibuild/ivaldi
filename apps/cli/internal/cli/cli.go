package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apitoken"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/auth"
	"github.com/ivaldibuild/ivaldi-cli/internal/helpers"
	"github.com/ivaldibuild/ivaldi-cli/internal/release"
	"github.com/ivaldibuild/ivaldi-cli/internal/serviceaccount"
	"github.com/ivaldibuild/ivaldi-cli/internal/types"
)

// The build stamps these with -ldflags "-X
// github.com/ivaldibuild/ivaldi-cli/internal/cli.<name>=<value>" (see the
// Makefile and .github/workflows/release.yaml). They are variables, not
// constants, because the linker can only set variables.
var (
	// VERSION is the release version, a SemVer.
	VERSION = "dev"
	// COMMIT is the short commit the binary was built from.
	COMMIT = "unknown"
	// BUILDTIME is that commit's time, RFC 3339.
	BUILDTIME = "unknown"
)

// Version is what `ivaldictl version` prints: the release version, and the
// commit it was built from when the build stamped one.
func Version() string {
	if COMMIT == "" || COMMIT == "unknown" {
		return VERSION
	}
	return fmt.Sprintf("%s (%s, %s)", VERSION, COMMIT, BUILDTIME)
}

// NewCLI returns ivaldictl's root command for the real process.
func NewCLI() *cobra.Command {
	return New(app.Default())
}

// New returns ivaldictl's root command running in a.
func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ivaldictl",
		Aliases:      []string{"iv", "ivldctl"},
		Short:        "Ivaldi CLI",
		Long:         "Ivaldi CLI controls the Ivaldi control plane",
		Version:      Version(),
		SilenceUsage: true,
	}
	cmd.SetOut(a.Out)

	helpers.AddPersistentFlags(cmd, initPersistentFlags())

	cmd.AddCommand(
		auth.New(a),
		release.New(a),
		serviceaccount.New(a),
		apitoken.New(a),
	)

	return cmd
}

func initPersistentFlags() map[string]types.Flag {
	return map[string]types.Flag{
		"server-url": {
			Type:        "string",
			Short:       "s",
			Description: "URL of the Ivaldi control plane; defaults to IVALDI_SERVER_URL, then " + app.DefaultServer,
			Default:     "",
		},
		"api-token": {
			Type:        "string",
			Short:       "a",
			Description: "A service account's API token; defaults to IVALDI_API_TOKEN. Without one, ivaldictl uses the session from ivaldictl auth login",
			Default:     "",
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
			Description: "Output format: json or text",
			Default:     "json",
		},
	}
}
