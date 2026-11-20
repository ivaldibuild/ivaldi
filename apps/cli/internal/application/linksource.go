package application

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func linkSourceCommand(a *app.App) *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "link-source APPLICATION",
		Short: "Link or unlink the Source an Application is built from",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.LinkApplicationSourceWithResponse(cmd.Context(), args[0],
				apiclient.LinkApplicationSourceJSONRequestBody{SourceId: source})
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printApplication(a, cmd, resp.JSON200.Data)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "The Source's ID; an empty value unlinks (required)")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}
