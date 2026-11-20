package project

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func transferCommand(a *app.App) *cobra.Command {
	var team string
	cmd := &cobra.Command{
		Use:   "transfer PROJECT_ID",
		Short: "Hand a Project to another Team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.TransferProjectWithResponse(cmd.Context(), args[0],
				apiclient.TransferProjectJSONRequestBody{OwnerTeamId: team})
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printProject(a, cmd, resp.JSON200.Data)
		},
	}
	cmd.Flags().StringVar(&team, "team", "", "The Team to hand the Project to (required)")
	_ = cmd.MarkFlagRequired("team")
	return cmd
}
