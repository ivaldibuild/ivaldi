package project

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

func listCommand(a *app.App) *cobra.Command {
	var workspace, search, team, sortBy, sortOrder string
	var all bool
	var limit int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a Workspace's Projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := &apiclient.ListProjectsParams{
				Limit:           &limit,
				Q:               optional.Of(search),
				OwnerTeamId:     optional.Of(team),
				IncludeArchived: optional.Of(all),
			}
			if sortBy != "" {
				s := apiclient.ListProjectsParamsSortBy(sortBy)
				if !s.Valid() {
					return fmt.Errorf("--sort %q must be created_at, slug, or display_name", sortBy)
				}
				params.SortBy = &s
			}
			if sortOrder != "" {
				o := apiclient.ListProjectsParamsSortOrder(sortOrder)
				if !o.Valid() {
					return fmt.Errorf("--order %q must be asc or desc", sortOrder)
				}
				params.SortOrder = &o
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ListProjectsWithResponse(cmd.Context(), workspace, params)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			page := resp.JSON200.Data
			return a.Print(cmd, page, func(w io.Writer) {
				for _, p := range page.Items {
					fmt.Fprintf(w, "%-32s %-24s %s\n", p.Id, p.Slug, p.DisplayName)
				}
				fmt.Fprintf(w, "%d of %d\n", len(page.Items), page.TotalCount)
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&workspace, "workspace", "", "The Workspace's ID (required)")
	fl.StringVarP(&search, "search", "q", "", "Keep the Projects whose name or slug contains this")
	fl.StringVar(&team, "team", "", "Only Projects this Team owns")
	fl.BoolVar(&all, "all", false, "Include archived Projects")
	fl.Int64Var(&limit, "limit", 20, "Most Projects to show")
	fl.StringVar(&sortBy, "sort", "", "Sort by created_at (the default), slug, or display_name")
	fl.StringVar(&sortOrder, "order", "", "Sort direction: asc or desc")
	_ = cmd.MarkFlagRequired("workspace")
	return cmd
}
