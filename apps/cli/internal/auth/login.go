package auth

import (
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/ciinfo"
	"github.com/ivaldibuild/ivaldi-cli/internal/oauth"
	"github.com/spf13/cobra"
)

func loginCommand(a *app.App) *cobra.Command {
	var organization string
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in through your browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ciinfo.Detect(a.Getenv).InCI {
				return errors.New("ivaldictl does not sign in through a browser in CI; set IVALDI_API_TOKEN to a service account's API token")
			}
			server, err := a.Server(cmd)
			if err != nil {
				return err
			}
			store, err := a.CredentialStore()
			if err != nil {
				return err
			}
			flow := oauth.Flow{
				Server: server, HTTP: a.HTTP, Organization: organization,
				OpenBrowser: a.OpenBrowser, Out: cmd.ErrOrStderr(), Now: a.Now,
			}
			if noBrowser {
				flow.OpenBrowser = nil
			}
			session, err := flow.Login(cmd.Context())
			if err != nil {
				return err
			}
			if err := store.Save(server, session); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Signed in to %s.\n", server)
			return nil
		},
	}

	cmd.Flags().StringVar(&organization, "org", "", "The Organization to sign in to, by slug or ID, when you belong to several")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the sign-in address instead of opening a browser")

	return cmd
}
