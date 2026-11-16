// Package change records Changes: the commits in a Source and the Session
// and Work Item that produced them, so release notes can name the work.
package change

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/ciinfo"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Record which work produced a Source's commits",
	}
	cmd.AddCommand(recordCommand(a))
	return cmd
}

type recordFlags struct {
	source, title, session, workItem, externalRef string
	commits                                       []string
}

func recordCommand(a *app.App) *cobra.Command {
	var f recordFlags
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record a Change: commits and the Work Item or Session they belong to",
		Long: `Records a Change in a Source: one or more commits, with the Work Item they serve
and the Session that produced them. When a Release is deployed, its release notes
list each commit with the Change that holds it.

In GitHub Actions and GitLab CI, the commit defaults to the one the pipeline runs
on. Run it where that commit is the one that lands, such as a push to main, not a
pull request's merge preview. A commit already recorded in the Source is refused.`,
		Example: `  # After a pull request merges
  ivaldictl change record --source src_01H... --title "Add checkout" \
    --work-item wki_01H... --external-ref https://github.com/acme/api/pull/42`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := f.body(ciinfo.Detect(a.Getenv))
			if err != nil {
				return err
			}
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.RecordChangeWithResponse(cmd.Context(), f.source, body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}
			change := resp.JSON201.Data
			return a.Print(cmd, change, func(w io.Writer) {
				fmt.Fprintf(w, "Change %s (%s) holds %d commit(s) in Source %s.\n",
					change.Title, change.Id, len(change.CommitShas), change.SourceId)
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.source, "source", "", "The Source's ID (required)")
	fl.StringVar(&f.title, "title", "", "What the change does, such as a pull request's title (required)")
	fl.StringArrayVar(&f.commits, "commit", nil, "A full commit SHA the Change holds; repeat for each. Defaults to CI's commit")
	fl.StringVar(&f.workItem, "work-item", "", "The Work Item the change serves")
	fl.StringVar(&f.session, "session", "", "The Session that produced the change")
	fl.StringVar(&f.externalRef, "external-ref", "", "Where the change is reviewed, such as a pull request's URL")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

// body builds the request from the flags, taking the commit from CI when
// none is given.
func (f recordFlags) body(ci ciinfo.Info) (apiclient.RecordChangeJSONRequestBody, error) {
	commits := f.commits
	if len(commits) == 0 && ci.CommitSHA != "" {
		commits = []string{ci.CommitSHA}
	}
	if len(commits) == 0 {
		return apiclient.RecordChangeJSONRequestBody{}, errors.New("give --commit: no CI commit was detected")
	}
	title := strings.TrimSpace(f.title)
	if title == "" {
		return apiclient.RecordChangeJSONRequestBody{}, errors.New("--title must not be empty")
	}
	return apiclient.RecordChangeJSONRequestBody{
		CommitShas:  commits,
		Title:       title,
		WorkItemId:  optional.Of(f.workItem),
		SessionId:   optional.Of(f.session),
		ExternalRef: optional.Of(f.externalRef),
	}, nil
}
