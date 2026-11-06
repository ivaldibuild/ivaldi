// Package auth signs ivaldictl in and out of a control plane.
package auth

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to and out of the Ivaldi control plane",
		Long: `ivaldictl authenticates in one of two ways:

  - Interactively, with "ivaldictl auth login", which signs you in through your
    browser and keeps the session for later commands.
  - With a service account's API token, given by --api-token or the
    IVALDI_API_TOKEN environment variable. CI uses this: in CI, ivaldictl never
    opens a browser.`,
	}

	cmd.AddCommand(
		loginCommand(a),
		statusCommand(a),
		logoutCommand(a),
	)

	return cmd
}
