// Package application creates and manages a Project's Applications.
package application

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "application",
		Aliases: []string{"app"},
		Short:   "Create and manage a Project's Applications",
		Long: "Create and manage Applications. An Application is software in a Project, built\n" +
			"from a Source and released through its Channels: see ivaldictl release.",
	}

	cmd.AddCommand(
		createCommand(a),
		listCommand(a),
		getCommand(a),
		updateCommand(a),
		linkSourceCommand(a),
		transferCommand(a),
		archiveCommand(a),
		restoreCommand(a),
	)

	return cmd
}

// changeCommand builds a command that archives or restores one Application.
// use names the argument too, since restore takes only an ID.
func changeCommand(a *app.App, use, short string,
	call func(*apiclient.ClientWithResponses, context.Context, string) (client.Response, *apiclient.ResponseApplicationSnapshot, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, data, err := call(c, cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, data != nil); err != nil {
				return err
			}
			return printApplication(a, cmd, data.Data)
		},
	}
}

// printApplication writes an Application as JSON, or for -o text, a summary.
func printApplication(a *app.App, cmd *cobra.Command, app apiclient.ApplicationSnapshot) error {
	return a.Print(cmd, app, func(w io.Writer) {
		status := "active"
		if app.ArchivedAt != nil {
			status = "archived"
		}
		fmt.Fprintf(w, "Application %s (%s) is %s, in Project %s.\n", app.DisplayName, app.Id, status, app.ProjectId)
		if app.SourceId != nil && *app.SourceId != "" {
			fmt.Fprintf(w, "Built from Source %s.\n", *app.SourceId)
		}
	})
}
