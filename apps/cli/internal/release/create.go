package release

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/ciinfo"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

type createCmdFlags struct {
	app              string
	version          string
	channel          string
	buildVersion     string
	commit           string
	ref              string
	ciProvider       string
	ciRun            string
	artifacts        []string
	triggeredBy      string
	triggeredByEmail string
	publish          bool
}

func createCommand(a *app.App) *cobra.Command {
	var flags createCmdFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Release from the Builds CI produced",
		Long: `Creates a Release of an Application from its Builds, each named by digest.

In GitHub Actions and GitLab CI, the commit, the Git ref (a branch, a tag, or a
pull or merge request), and the CI run are read from the environment; flags
override them. On a version tag such as v1.4.0, the version defaults to 1.4.0.

Without --channel, the Release joins the one Channel whose rules it meets.
With --publish it is published at once, as a tag pipeline would.`,
		Example: `  # From a tag pipeline: create and publish
  ivaldictl release create --app app_01H... --artifact api=sha256:4f2a... --publish

  # From a pull request: a draft, with an explicit version
  ivaldictl release create --app app_01H... --version 1.5.0-pr.42 --artifact api=sha256:9c1d...`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := flags.body(ciinfo.Detect(a.Getenv))
			if err != nil {
				return err
			}
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.CreateReleaseWithResponse(cmd.Context(), flags.app, body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}
			return printRelease(a, cmd, resp.JSON201.Data)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&flags.app, "app", "", "The Application's ID or slug (required)")
	fl.StringVar(&flags.version, "version", "", "The Release's version; defaults to the version a v-prefixed tag names")
	fl.StringArrayVar(&flags.artifacts, "artifact", nil, "An artifact's Build as name=digest, such as api=sha256:4f2a...; repeat for each artifact")
	fl.StringVar(&flags.channel, "channel", "", "The Channel's ID; by default the one whose rules the Release meets")
	fl.BoolVar(&flags.publish, "publish", false, "Publish the Release as soon as it is created")
	fl.StringVar(&flags.buildVersion, "build-version", "", "The version to record on each Build; defaults to the Release's version")
	fl.StringVar(&flags.commit, "commit", "", "The commit the Builds were built from; detected in CI")
	fl.StringVar(&flags.ref, "ref", "", "The full Git ref the Builds were built from, such as refs/tags/v1.4.0; detected in CI")
	fl.StringVar(&flags.ciProvider, "ci-provider", "", "The CI system, such as github_actions; detected in CI")
	fl.StringVar(&flags.ciRun, "ci-run", "", "The CI run's URL; detected in CI")
	fl.StringVar(&flags.triggeredBy, "triggered-by", "", "Who triggered the Release, such as the CI user who ran the pipeline; detected in CI")
	fl.StringVar(&flags.triggeredByEmail, "triggered-by-email", "", "The email address of whoever triggered the Release; detected in GitLab CI. Giving either flag ignores what CI says about both")

	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("artifact")

	return cmd
}

// body builds the request from the flags, filling in what CI says.
func (flags createCmdFlags) body(ci ciinfo.Info) (apiclient.CreateReleaseJSONRequestBody, error) {
	var body apiclient.CreateReleaseJSONRequestBody

	ref := or(flags.ref, ci.Ref)
	if ref != "" && !strings.HasPrefix(ref, "refs/") {
		return body, fmt.Errorf("--ref %q must be a full Git ref, such as refs/heads/%s", ref, ref)
	}

	version := or(flags.version, ciinfo.VersionFromRef(ref))
	if version == "" {
		return body, errors.New("give --version: it can be read only from a version tag such as refs/tags/v1.4.0")
	}

	body.Version = version
	body.ChannelId = optional.Of(flags.channel)

	if flags.publish {
		body.Publish = &flags.publish
	}

	// The name and email describe one person, so they come from the flags or
	// from CI, never one from each.
	triggeredBy, triggeredByEmail := ci.TriggeredBy, ci.TriggeredByEmail
	if flags.triggeredBy != "" || flags.triggeredByEmail != "" {
		triggeredBy, triggeredByEmail = flags.triggeredBy, flags.triggeredByEmail
	}
	body.TriggeredBy = optional.Of(triggeredBy)
	body.TriggeredByEmail = optional.Of(triggeredByEmail)

	builds := []apiclient.BuildBody{}
	seen := map[string]bool{}

	for _, spec := range flags.artifacts {
		name, digest, ok := strings.Cut(spec, "=")
		if !ok || name == "" || digest == "" {
			return body, fmt.Errorf("--artifact %q must be name=digest, such as api=sha256:4f2a...", spec)
		}

		if !strings.Contains(digest, ":") {
			return body, fmt.Errorf("--artifact %s: %q is not a digest; tags cannot be resolved yet, so give the digest, such as sha256:4f2a...", name, digest)
		}

		if seen[name] {
			return body, fmt.Errorf("--artifact %s is given twice", name)
		}

		seen[name] = true
		builds = append(builds, apiclient.BuildBody{
			Artifact:   name,
			Digest:     optional.Of(digest),
			Version:    optional.Of(or(flags.buildVersion, version)),
			CommitSha:  optional.Of(or(flags.commit, ci.CommitSHA)),
			SourceRef:  optional.Of(ref),
			CiProvider: optional.Of(or(flags.ciProvider, ci.Provider)),
			CiRunRef:   optional.Of(or(flags.ciRun, ci.RunURL)),
		})
	}

	body.Builds = builds

	return body, nil
}
