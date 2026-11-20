package application

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

func listCommand(a *app.App) *cobra.Command {
	var project, workspace, team, sortBy, sortOrder string
	var all bool
	var limit int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a Project's or a Workspace's Applications",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (project == "") == (workspace == "") {
				return errors.New("give exactly one of --project or --workspace")
			}
			// Both list operations take the same sort orders.
			order := apiclient.ListApplicationsParamsSortOrder(sortOrder)
			if sortOrder != "" && !order.Valid() {
				return fmt.Errorf("--order %q must be asc or desc", sortOrder)
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}

			var page apiclient.ResultApplicationSnapshot
			if project != "" {
				params := &apiclient.ListApplicationsParams{
					Limit:           &limit,
					OwnerTeamId:     optional.Of(team),
					IncludeArchived: optional.Of(all),
					SortBy:          optional.Of(sortBy),
					SortOrder:       optional.Of(order),
				}
				resp, err := c.ListApplicationsWithResponse(cmd.Context(), project, params)
				if err != nil {
					return err
				}
				if err := client.Check(resp, resp.JSON200 != nil); err != nil {
					return err
				}
				page = resp.JSON200.Data
			} else {
				params := &apiclient.ListWorkspaceApplicationsParams{
					Limit:           &limit,
					OwnerTeamId:     optional.Of(team),
					IncludeArchived: optional.Of(all),
					SortBy:          optional.Of(sortBy),
					SortOrder:       optional.Of(apiclient.ListWorkspaceApplicationsParamsSortOrder(sortOrder)),
				}
				resp, err := c.ListWorkspaceApplicationsWithResponse(cmd.Context(), workspace, params)
				if err != nil {
					return err
				}
				if err := client.Check(resp, resp.JSON200 != nil); err != nil {
					return err
				}
				page = resp.JSON200.Data
			}

			return a.Print(cmd, page, func(w io.Writer) {
				for _, app := range page.Items {
					fmt.Fprintf(w, "%-32s %-24s %s\n", app.Id, app.Slug, app.DisplayName)
				}
				fmt.Fprintf(w, "%d of %d\n", len(page.Items), page.TotalCount)
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&project, "project", "", "The Project's ID")
	fl.StringVar(&workspace, "workspace", "", "The Workspace's ID")
	fl.StringVar(&team, "team", "", "Only Applications this Team owns")
	fl.BoolVar(&all, "all", false, "Include archived Applications")
	fl.Int64Var(&limit, "limit", 20, "Most Applications to show")
	fl.StringVar(&sortBy, "sort", "", "Sort by created_at (the default), slug, or display_name")
	fl.StringVar(&sortOrder, "order", "", "Sort direction: asc or desc")
	return cmd
}
