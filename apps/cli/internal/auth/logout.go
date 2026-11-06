package auth

import (
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/credentials"
	"github.com/ivaldibuild/ivaldi-cli/internal/oauth"
	"github.com/spf13/cobra"
)

func logoutCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "End the signed-in session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			server, err := a.Server(cmd)
			if err != nil {
				return err
			}

			store, err := a.CredentialStore()
			if err != nil {
				return err
			}

			session, err := store.Load(server)
			if errors.Is(err, credentials.ErrNoSession) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Not signed in to %s.\n", server)
				return nil
			}

			if err != nil {
				return err
			}

			// Revoking is best effort: the local session goes either way.
			revokeErr := oauth.Revoke(cmd.Context(), a.HTTP, server, session.RefreshToken)
			if err := store.Delete(server); err != nil {
				return err
			}

			if revokeErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Signed out locally; the control plane could not revoke the session: %v\n", revokeErr)
				return nil
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Signed out of %s.\n", server)

			return nil
		},
	}
}
