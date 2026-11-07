// Package ciinfo reads what a CI system says about the build it runs: the
// commit, the full Git ref, the run, and who triggered it, so a Release can
// record where its Build came from without the pipeline spelling it out.
package ciinfo

import (
	"regexp"
	"strings"
)

// Info is what a CI system's environment says about the current build.
type Info struct {
	// Provider names the CI system, such as github_actions or gitlab_ci, or
	// "" outside a known one.
	Provider string
	// InCI reports whether the process runs in CI at all, known or not.
	InCI      bool
	CommitSHA string
	// Ref is the full Git ref: refs/heads/main, refs/tags/v1.4.0,
	// refs/pull/42/merge, or refs/merge-requests/7/head.
	Ref string
	// RunURL links to the CI run.
	RunURL string
	// TriggeredBy is the CI user who started the run, and TriggeredByEmail
	// their email address where the CI system exposes it (GitLab does,
	// GitHub Actions does not).
	TriggeredBy      string
	TriggeredByEmail string
}

// Detect reads Info from the environment through getenv.
func Detect(getenv func(string) string) Info {
	switch {
	case getenv("GITHUB_ACTIONS") == "true":
		info := Info{
			Provider:  "github_actions",
			InCI:      true,
			CommitSHA: getenv("GITHUB_SHA"),
			Ref:       getenv("GITHUB_REF"),
			// A re-run keeps GITHUB_ACTOR from the first run; the triggering
			// actor is whoever started this one.
			TriggeredBy: or(getenv("GITHUB_TRIGGERING_ACTOR"), getenv("GITHUB_ACTOR")),
		}
		if server, repo, run := getenv("GITHUB_SERVER_URL"), getenv("GITHUB_REPOSITORY"), getenv("GITHUB_RUN_ID"); server != "" && repo != "" && run != "" {
			info.RunURL = server + "/" + repo + "/actions/runs/" + run
		}
		return info
	case getenv("GITLAB_CI") == "true":
		info := Info{
			Provider:         "gitlab_ci",
			InCI:             true,
			CommitSHA:        getenv("CI_COMMIT_SHA"),
			RunURL:           getenv("CI_PIPELINE_URL"),
			TriggeredBy:      getenv("GITLAB_USER_LOGIN"),
			TriggeredByEmail: getenv("GITLAB_USER_EMAIL"),
		}
		switch {
		case getenv("CI_COMMIT_TAG") != "":
			info.Ref = "refs/tags/" + getenv("CI_COMMIT_TAG")
		case getenv("CI_MERGE_REQUEST_IID") != "":
			info.Ref = "refs/merge-requests/" + getenv("CI_MERGE_REQUEST_IID") + "/head"
		case getenv("CI_COMMIT_BRANCH") != "":
			info.Ref = "refs/heads/" + getenv("CI_COMMIT_BRANCH")
		}
		return info
	}
	return Info{InCI: isTrue(getenv("CI"))}
}

// or returns a, or b when a is empty.
func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// versionTag is a tag that names a version: an optional v, then a digit.
var versionTag = regexp.MustCompile(`^v?[0-9]`)

// VersionFromRef returns the version a tag ref names, without a leading v,
// such as 1.4.0 for refs/tags/v1.4.0. It returns "" for a ref that is not a
// version tag.
func VersionFromRef(ref string) string {
	tag, ok := strings.CutPrefix(ref, "refs/tags/")
	if !ok || !versionTag.MatchString(tag) {
		return ""
	}
	return strings.TrimPrefix(tag, "v")
}
