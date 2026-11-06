package release

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusMoves(t *testing.T) {
	assert.True(t, CanMove(StatusDraft, StatusPublished))
	assert.True(t, CanMove(StatusPublished, StatusWithdrawn))
	assert.False(t, CanMove(StatusDraft, StatusWithdrawn), "a draft is never withdrawn")
	assert.False(t, CanMove(StatusPublished, StatusDraft), "publishing is final")
	assert.False(t, CanMove(StatusWithdrawn, StatusPublished))
	assert.Error(t, CheckMove(StatusWithdrawn, StatusPublished))
	assert.True(t, StatusDraft.Valid())
	assert.False(t, Status("deleted").Valid())
}

func TestMatchRef(t *testing.T) {
	cases := []struct {
		pattern, ref string
		want         bool
	}{
		{"refs/heads/main", "refs/heads/main", true},
		{"refs/heads/main", "refs/heads/main2", false},
		{"refs/heads/*", "refs/heads/main", true},
		{"refs/heads/*", "refs/heads/hotfix/x", false},
		{"refs/heads/hotfix/**", "refs/heads/hotfix/a/b", true},
		{"refs/tags/v*", "refs/tags/v1.4.0", true},
		{"refs/pull/*/head", "refs/pull/42/head", true},
		{"refs/pull/*/head", "refs/pull/42/merge", false},
		{"refs/heads/release-?", "refs/heads/release-1", true},
		{"refs/heads/[a-c]*", "refs/heads/beta", true},
		{"refs/heads/[^a-c]*", "refs/heads/beta", false},
		{"refs/heads/a.b", "refs/heads/aXb", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, MatchRef(tc.pattern, tc.ref), "%s ~ %s", tc.pattern, tc.ref)
	}
}

func rule(kind RuleKind, artifactID *string, config string) Rule {
	return Rule{Kind: kind, ArtifactID: artifactID, Config: json.RawMessage(config)}
}

func TestAdmits(t *testing.T) {
	api := "art_api"
	candidate := Candidate{
		Version: "2.1.0-beta.1",
		Builds: []CandidateBuild{
			{ArtifactID: "art_api", ArtifactName: "api", Version: "2.1.0", SourceRef: "refs/tags/v2.1.0-beta.1"},
			{ArtifactID: "art_sidecar", ArtifactName: "sidecar", SourceRef: "refs/heads/main"},
		},
	}
	cases := map[string]struct {
		rules   []Rule
		ok      bool
		reasons []string
	}{
		"no rules admit everything": {ok: true},
		"a range and a matching pre-release": {
			rules: []Rule{rule(RuleVersion, nil, `{"range":"[2.0,3.0)","pre_release":"^beta"}`)}, ok: true,
		},
		"no pre-release wanted": {
			rules:   []Rule{rule(RuleVersion, nil, `{"range":"[2.0,3.0)","pre_release":"^$"}`)},
			reasons: []string{`the release version 2.1.0-beta.1 has pre-release tag "beta.1", which does not match ^$`},
		},
		"out of range": {
			rules:   []Rule{rule(RuleVersion, nil, `{"range":"[3.0,)"}`)},
			reasons: []string{"the release version 2.1.0-beta.1 is not in [3.0,)"},
		},
		"an artifact's build version": {
			rules: []Rule{rule(RuleVersion, &api, `{"range":"[2.1]","pre_release":"^$"}`)}, ok: true,
		},
		"a version pattern": {
			rules:   []Rule{rule(RuleVersion, nil, `{"pattern":"^build-\\d+$"}`)},
			reasons: []string{`the release version "2.1.0-beta.1" does not match ^build-\d+$`},
		},
		"every build's ref must match": {
			rules:   []Rule{rule(RuleGitRef, nil, `{"patterns":["refs/tags/v*"]}`)},
			reasons: []string{"the sidecar build's ref refs/heads/main does not match refs/tags/v*"},
		},
		"one artifact's ref": {
			rules: []Rule{rule(RuleGitRef, &api, `{"patterns":["refs/tags/v*"]}`)}, ok: true,
		},
		"a rule for an artifact without a build": {
			rules:   []Rule{rule(RuleGitRef, new("art_chart"), `{"patterns":["refs/tags/v*"]}`)},
			reasons: []string{"the rule needs a build of an artifact the release does not include"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ok, reasons, err := Admits(tc.rules, candidate)
			require.NoError(t, err)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.reasons, reasons)
		})
	}

	t.Run("a version that does not parse fails a range but not a pattern", func(t *testing.T) {
		c := Candidate{Version: "build-12"}
		ok, reasons, err := Admits([]Rule{rule(RuleVersion, nil, `{"range":"1.0"}`)}, c)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, []string{`the release version "build-12" is not a version a range can compare`}, reasons)
		ok, _, err = Admits([]Rule{rule(RuleVersion, nil, `{"pattern":"^build-\\d+$"}`)}, c)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a build without a ref fails a git_ref rule", func(t *testing.T) {
		c := Candidate{Builds: []CandidateBuild{{ArtifactID: "a", ArtifactName: "api"}}}
		_, reasons, err := Admits([]Rule{rule(RuleGitRef, nil, `{"patterns":["refs/heads/main"]}`)}, c)
		require.NoError(t, err)
		assert.Equal(t, []string{"the api build has no Git ref to match refs/heads/main"}, reasons)
	})

	t.Run("an invalid rule is an error", func(t *testing.T) {
		_, _, err := Admits([]Rule{rule(RuleVersion, nil, `{}`)}, candidate)
		assert.ErrorIs(t, err, ErrInvalidRule)
		_, _, err = Admits([]Rule{rule("size", nil, `{}`)}, candidate)
		assert.ErrorIs(t, err, ErrInvalidRule)
	})
}
