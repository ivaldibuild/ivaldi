package release

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func withdrawCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "withdraw RELEASE_ID",
		Short: "Withdraw a published Release so it is not deployed again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.WithdrawReleaseWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printRelease(a, cmd, resp.JSON200.Data)
		},
	}
}
