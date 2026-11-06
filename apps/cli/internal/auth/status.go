package auth

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func statusCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who ivaldictl acts as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.GetMeWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			who := resp.JSON200.Data

			method := "a signed-in session"
			if a.APIToken(cmd) != "" {
				method = "an API token"
			}
			return a.Print(cmd, who, func(w io.Writer) {
				fmt.Fprintf(w, "Acting as %s (%s) in %s, with %s.\n", who.Principal.DisplayName, who.Principal.Kind, who.Organization.DisplayName, method)
				for _, r := range who.Roles {
					fmt.Fprintf(w, "  %s of the Organization\n", r.Role)
				}
			})
		},
	}
}
