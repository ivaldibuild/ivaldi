package application

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func updateCommand(a *app.App) *cobra.Command {
	var name, slug string
	cmd := &cobra.Command{
		Use:   "update APPLICATION",
		Short: "Change an Application's name or slug",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			var body apiclient.UpdateApplicationJSONRequestBody
			if fl.Changed("name") {
				body.DisplayName = &name
			}
			if fl.Changed("slug") {
				body.Slug = &slug
			}
			if body.DisplayName == nil && body.Slug == nil {
				return errors.New("give --name or --slug to change")
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.UpdateApplicationWithResponse(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printApplication(a, cmd, resp.JSON200.Data)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "New display name")
	fl.StringVar(&slug, "slug", "", "New slug")
	return cmd
}
