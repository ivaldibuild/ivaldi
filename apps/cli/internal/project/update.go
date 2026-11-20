package project

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func updateCommand(a *app.App) *cobra.Command {
	var name, slug, description string
	cmd := &cobra.Command{
		Use:   "update PROJECT_ID",
		Short: "Rename or redescribe a Project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			var body apiclient.UpdateProjectJSONRequestBody
			if fl.Changed("name") {
				body.DisplayName = &name
			}
			if fl.Changed("slug") {
				body.Slug = &slug
			}
			if fl.Changed("description") {
				body.Description = &description
			}
			if body.DisplayName == nil && body.Slug == nil && body.Description == nil {
				return errors.New("give --name, --slug, or --description to change")
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.UpdateProjectWithResponse(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printProject(a, cmd, resp.JSON200.Data)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "New display name")
	fl.StringVar(&slug, "slug", "", "New slug")
	fl.StringVar(&description, "description", "", "New description; an empty value clears it")
	return cmd
}
