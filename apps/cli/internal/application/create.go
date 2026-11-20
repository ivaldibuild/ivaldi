package application

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

func createCommand(a *app.App) *cobra.Command {
	var project, name, slug, team, source, lifecycle string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Application in a Project, with its default Channel",
		Long: "Create an Application in a Project. Its slug and owning Team default to a slug\n" +
			"derived from the name and the Project's owning Team.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := apiclient.CreateApplicationJSONRequestBody{
				DisplayName:        name,
				Slug:               optional.Of(slug),
				OwnerTeamId:        optional.Of(team),
				SourceId:           optional.Of(source),
				DefaultLifecycleId: optional.Of(lifecycle),
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.CreateApplicationWithResponse(cmd.Context(), project, body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}
			created := resp.JSON201.Data
			return a.Print(cmd, created, func(w io.Writer) {
				fmt.Fprintf(w, "Application %s (%s) is active, with default Channel %s (%s).\n",
					created.Application.DisplayName, created.Application.Id,
					created.DefaultChannel.DisplayName, created.DefaultChannel.Id)
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&project, "project", "", "The Project's ID (required)")
	fl.StringVar(&name, "name", "", "The Application's display name (required)")
	fl.StringVar(&slug, "slug", "", "URL slug; derived from the name when omitted")
	fl.StringVar(&team, "team", "", "The owning Team's ID; the Project's owning Team when omitted")
	fl.StringVar(&source, "source", "", "The ID of the Source the Application is built from")
	fl.StringVar(&lifecycle, "lifecycle", "", "The ID of the Lifecycle the default Channel follows; the Workspace's default Lifecycle when omitted")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
