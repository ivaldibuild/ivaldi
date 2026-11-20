package project

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func archiveCommand(a *app.App) *cobra.Command {
	return changeCommand(a, "archive", "Archive a Project that has nothing active in it",
		func(c *apiclient.ClientWithResponses, ctx context.Context, id string) (client.Response, *apiclient.ResponseProjectSnapshot, error) {
			resp, err := c.ArchiveProjectWithResponse(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			return resp, resp.JSON200, nil
		})
}
