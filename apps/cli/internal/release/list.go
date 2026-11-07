package release

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func listCommand(a *app.App) *cobra.Command {
	var appID, status, channel string
	var limit int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List an Application's Releases, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := &apiclient.ListReleasesParams{Limit: &limit, ChannelId: optional(channel)}
			desc := apiclient.ListReleasesParamsSortOrderDesc
			params.SortOrder = &desc
			if status != "" {
				s := apiclient.ListReleasesParamsStatus(status)
				if !s.Valid() {
					return fmt.Errorf("--status %q must be draft, published, or withdrawn", status)
				}
				params.Status = &s
			}

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}

			resp, err := c.ListReleasesWithResponse(cmd.Context(), appID, params)
			if err != nil {
				return err
			}

			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}

			page := resp.JSON200.Data
			return a.Print(cmd, page, func(w io.Writer) {
				for _, r := range page.Items {
					fmt.Fprintf(w, "%-24s %-10s %s\n", r.Version, r.Status, r.Id)
				}
				fmt.Fprintf(w, "%d of %d\n", len(page.Items), page.TotalCount)
			})
		},
	}

	cmd.Flags().StringVar(&appID, "app", "", "The Application's ID or slug (required)")
	cmd.Flags().StringVar(&status, "status", "", "Only Releases with this status: draft, published, or withdrawn")
	cmd.Flags().StringVar(&channel, "channel", "", "Only Releases in this Channel")
	cmd.Flags().Int64Var(&limit, "limit", 20, "Most Releases to show")
	_ = cmd.MarkFlagRequired("app")

	return cmd
}
