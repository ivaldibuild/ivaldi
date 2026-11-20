package project

import (
	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

func createCommand(a *app.App) *cobra.Command {
	var workspace, name, slug, team, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Project in a Workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			body := apiclient.CreateProjectJSONRequestBody{
				OwnerTeamId: team,
				Slug:        slug,
				DisplayName: name,
				Description: optional.Of(description),
			}
			resp, err := c.CreateProjectWithResponse(cmd.Context(), workspace, body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}
			return printProject(a, cmd, resp.JSON201.Data)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&workspace, "workspace", "", "The Workspace's ID (required)")
	fl.StringVar(&name, "name", "", "The Project's display name (required)")
	fl.StringVar(&slug, "slug", "", "URL slug: lowercase letters, digits, and single hyphens (required)")
	fl.StringVar(&team, "team", "", "The owning Team's ID (required)")
	fl.StringVar(&description, "description", "", "What the Project is for")
	_ = cmd.MarkFlagRequired("workspace")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("slug")
	_ = cmd.MarkFlagRequired("team")
	return cmd
}
