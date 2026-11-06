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
)

type createFlags struct {
	app, version, channel, buildVersion string
	commit, ref, ciProvider, ciRun      string
	artifacts                           []string
	publish                             bool
}

func createCommand(a *app.App) *cobra.Command {
	var f createFlags
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
			body, err := f.body(ciinfo.Detect(a.Getenv))
			if err != nil {
				return err
			}
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.CreateReleaseWithResponse(cmd.Context(), f.app, body)
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
	fl.StringVar(&f.app, "app", "", "The Application's ID (required)")
	fl.StringVar(&f.version, "version", "", "The Release's version; defaults to the version a v-prefixed tag names")
	fl.StringArrayVar(&f.artifacts, "artifact", nil, "An artifact's Build as name=digest, such as api=sha256:4f2a...; repeat for each artifact")
	fl.StringVar(&f.channel, "channel", "", "The Channel's ID; by default the one whose rules the Release meets")
	fl.BoolVar(&f.publish, "publish", false, "Publish the Release as soon as it is created")
	fl.StringVar(&f.buildVersion, "build-version", "", "The version to record on each Build; defaults to the Release's version")
	fl.StringVar(&f.commit, "commit", "", "The commit the Builds were built from; detected in CI")
	fl.StringVar(&f.ref, "ref", "", "The full Git ref the Builds were built from, such as refs/tags/v1.4.0; detected in CI")
	fl.StringVar(&f.ciProvider, "ci-provider", "", "The CI system, such as github_actions; detected in CI")
	fl.StringVar(&f.ciRun, "ci-run", "", "The CI run's URL; detected in CI")
	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("artifact")
	return cmd
}

// body builds the request from the flags, filling in what CI says.
func (f createFlags) body(ci ciinfo.Info) (apiclient.CreateReleaseJSONRequestBody, error) {
	var body apiclient.CreateReleaseJSONRequestBody
	ref := or(f.ref, ci.Ref)
	if ref != "" && !strings.HasPrefix(ref, "refs/") {
		return body, fmt.Errorf("--ref %q must be a full Git ref, such as refs/heads/%s", ref, ref)
	}
	version := or(f.version, ciinfo.VersionFromRef(ref))
	if version == "" {
		return body, errors.New("give --version: it can be read only from a version tag such as refs/tags/v1.4.0")
	}
	body.Version = version
	body.ChannelId = optional(f.channel)
	if f.publish {
		body.Publish = &f.publish
	}
	builds := []apiclient.BuildBody{}
	seen := map[string]bool{}
	for _, spec := range f.artifacts {
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
			Digest:     optional(digest),
			Version:    optional(or(f.buildVersion, version)),
			CommitSha:  optional(or(f.commit, ci.CommitSHA)),
			SourceRef:  optional(ref),
			CiProvider: optional(or(f.ciProvider, ci.Provider)),
			CiRunRef:   optional(or(f.ciRun, ci.RunURL)),
		})
	}
	body.Builds = builds
	return body, nil
}
