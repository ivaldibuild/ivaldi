package ciinfo

import "testing"

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDetect(t *testing.T) {
	cases := map[string]struct {
		vars map[string]string
		want Info
	}{
		"github actions on a tag": {
			vars: map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SHA": "abc123", "GITHUB_REF": "refs/tags/v1.4.0",
				"GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "acme/api", "GITHUB_RUN_ID": "77"},
			want: Info{Provider: "github_actions", InCI: true, CommitSHA: "abc123", Ref: "refs/tags/v1.4.0", RunURL: "https://github.com/acme/api/actions/runs/77"},
		},
		"github actions on a pull request": {
			vars: map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SHA": "def", "GITHUB_REF": "refs/pull/42/merge"},
			want: Info{Provider: "github_actions", InCI: true, CommitSHA: "def", Ref: "refs/pull/42/merge"},
		},
		"gitlab on a tag": {
			vars: map[string]string{"GITLAB_CI": "true", "CI": "true", "CI_COMMIT_SHA": "abc", "CI_COMMIT_TAG": "v2.0.0", "CI_PIPELINE_URL": "https://gitlab.com/acme/api/-/pipelines/9"},
			want: Info{Provider: "gitlab_ci", InCI: true, CommitSHA: "abc", Ref: "refs/tags/v2.0.0", RunURL: "https://gitlab.com/acme/api/-/pipelines/9"},
		},
		"gitlab on a merge request": {
			vars: map[string]string{"GITLAB_CI": "true", "CI_MERGE_REQUEST_IID": "7", "CI_COMMIT_BRANCH": "feature"},
			want: Info{Provider: "gitlab_ci", InCI: true, Ref: "refs/merge-requests/7/head"},
		},
		"gitlab on a branch": {
			vars: map[string]string{"GITLAB_CI": "true", "CI_COMMIT_BRANCH": "main"},
			want: Info{Provider: "gitlab_ci", InCI: true, Ref: "refs/heads/main"},
		},
		"another CI":    {vars: map[string]string{"CI": "true"}, want: Info{InCI: true}},
		"a workstation": {vars: map[string]string{}, want: Info{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Detect(env(tc.vars)); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVersionFromRef(t *testing.T) {
	for ref, want := range map[string]string{
		"refs/tags/v1.4.0":     "1.4.0",
		"refs/tags/2.0.0-rc.1": "2.0.0-rc.1",
		"refs/tags/release-1":  "",
		"refs/heads/v1.4.0":    "",
		"refs/pull/42/merge":   "",
		"":                     "",
	} {
		if got := VersionFromRef(ref); got != want {
			t.Errorf("VersionFromRef(%q) = %q, want %q", ref, got, want)
		}
	}
}
