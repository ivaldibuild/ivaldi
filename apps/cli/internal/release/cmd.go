// Package release creates, publishes, and withdraws Releases.
package release

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Create and manage an Application's Releases",
	}

	cmd.AddCommand(
		createCommand(a),
		publishCommand(a),
		withdrawCommand(a),
		getCommand(a),
		listCommand(a),
	)

	return cmd
}

// printRelease writes a Release as JSON, or for -o text, a summary.
func printRelease(a *app.App, cmd *cobra.Command, r apiclient.Release) error {
	return a.Print(cmd, r, func(w io.Writer) {
		fmt.Fprintf(w, "Release %s (%s) is %s, in Channel %s.\n", r.Version, r.Id, r.Status, r.ChannelId)
		for _, b := range r.Builds {
			fmt.Fprintf(w, "  %s@%s\n", b.Build.ArtifactName, b.Build.Digest)
		}
	})
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
