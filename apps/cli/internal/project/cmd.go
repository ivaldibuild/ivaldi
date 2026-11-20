// Package project creates and manages a Workspace's Projects.
package project

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
		Use:   "project",
		Short: "Create and manage a Workspace's Projects",
		Long: "Create and manage a Workspace's Projects. A Project is a body of work in a\n" +
			"Workspace; its Applications are created with ivaldictl application.",
	}

	cmd.AddCommand(
		createCommand(a),
		listCommand(a),
		getCommand(a),
		updateCommand(a),
		transferCommand(a),
		archiveCommand(a),
		restoreCommand(a),
	)

	return cmd
}

// changeCommand builds a command that archives or restores one Project.
func changeCommand(a *app.App, use, short string,
	call func(*apiclient.ClientWithResponses, context.Context, string) (client.Response, *apiclient.ResponseProjectSnapshot, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use + " PROJECT_ID",
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
			return printProject(a, cmd, data.Data)
		},
	}
}

// printProject writes a Project as JSON, or for -o text, a summary.
func printProject(a *app.App, cmd *cobra.Command, p apiclient.ProjectSnapshot) error {
	return a.Print(cmd, p, func(w io.Writer) {
		status := "active"
		if p.ArchivedAt != nil {
			status = "archived"
		}
		fmt.Fprintf(w, "Project %s (%s) is %s.\n", p.DisplayName, p.Id, status)
		if p.Description != "" {
			fmt.Fprintln(w, p.Description)
		}
	})
}
