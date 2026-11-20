package application

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func getCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "get APPLICATION",
		Short: "Show an Application",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.GetApplicationWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printApplication(a, cmd, resp.JSON200.Data)
		},
	}
}
