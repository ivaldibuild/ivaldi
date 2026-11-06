package delivery

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

var domDigest = "sha256:" + strings.Repeat("ab", 32)

func domBuildProps() NewBuildProps {
	return NewBuildProps{
		OrganizationID: domOrg,
		ArtifactKind:   ArtifactContainerImage,
		ArtifactName:   "ghcr.io/acme/api",
		Digest:         domDigest,
		Provenance:     BuildCIReported,
		Details: BuildDetails{
			Version:      new("1.4.0"),
			CommitSHA:    new("0123456789abcdef0123456789abcdef01234567"),
			SourceRef:    new("refs/tags/v1.4.0"),
			CIProvider:   new("github_actions"),
			CIRunRef:     new("https://github.com/acme/api/actions/runs/1"),
			ReportedTags: []string{"v1.4.0", "latest", "v1.4.0"},
		},
	}
}

func TestNewBuild(t *testing.T) {
	clock.Fix(t, domT0)
	b, err := NewBuild(domBuildProps(), nil)
	require.NoError(t, err)
	assert.True(t, id.Valid("bld", b.ID()))
	s := b.Snapshot()
	assert.Equal(t, []string{"latest", "v1.4.0"}, s.ReportedTags, "sorted, each once")
	assert.Equal(t, BuildCIReported, s.Provenance)
	assert.Equal(t, []aggregate.Event{BuildRecorded{s}}, b.PullEvents())

	stored, err := NewBuild(domBuildProps(), new(b.ID()))
	require.NoError(t, err)
	assert.Empty(t, stored.PullEvents())

	for name, edit := range map[string]func(*NewBuildProps){
		"a tag as the digest":        func(p *NewBuildProps) { p.Digest = "v1.4.0" },
		"a short sha256":             func(p *NewBuildProps) { p.Digest = "sha256:abc" },
		"an upper-case sha256":       func(p *NewBuildProps) { p.Digest = "sha256:" + strings.Repeat("AB", 32) },
		"an unknown kind":            func(p *NewBuildProps) { p.ArtifactKind = "jar" },
		"a branch name for a ref":    func(p *NewBuildProps) { p.Details.SourceRef = new("main") },
		"a commit that is not hex":   func(p *NewBuildProps) { p.Details.CommitSHA = new("not-a-sha") },
		"a version with a space":     func(p *NewBuildProps) { p.Details.Version = new("1.4 beta") },
		"an invalid tag":             func(p *NewBuildProps) { p.Details.ReportedTags = []string{"-bad"} },
		"an observed build's commit": func(p *NewBuildProps) { p.Provenance = BuildObserved },
		"an unknown provenance":      func(p *NewBuildProps) { p.Provenance = "guessed" },
		"no artifact name":           func(p *NewBuildProps) { p.ArtifactName = "" },
	} {
		t.Run(name, func(t *testing.T) {
			props := domBuildProps()
			edit(&props)
			_, err := NewBuild(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("other digest algorithms are accepted", func(t *testing.T) {
		props := domBuildProps()
		props.Digest = "sha512:" + strings.Repeat("cd", 64)
		_, err := NewBuild(props, nil)
		require.NoError(t, err)
	})
}

func TestBuildReport(t *testing.T) {
	clock.Fix(t, domT0)

	t.Run("an observed build becomes CI-reported once", func(t *testing.T) {
		props := domBuildProps()
		props.Provenance = BuildObserved
		props.Details = BuildDetails{}
		b, err := NewBuild(props, nil)
		require.NoError(t, err)
		b.PullEvents()
		clock.Fix(t, domT1)

		changed, err := b.Report(domBuildProps().Details)
		require.NoError(t, err)
		assert.True(t, changed)
		s := b.Snapshot()
		assert.Equal(t, BuildCIReported, s.Provenance)
		assert.Equal(t, new("refs/tags/v1.4.0"), s.SourceRef)
		assert.Equal(t, domT1, s.UpdatedAt)
		assert.Equal(t, []aggregate.Event{BuildReported{s}}, b.PullEvents())
	})

	t.Run("a CI-reported build does not change", func(t *testing.T) {
		b, err := NewBuild(domBuildProps(), nil)
		require.NoError(t, err)
		b.PullEvents()
		again := domBuildProps().Details
		again.SourceRef = new("refs/heads/main")
		again.CommitSHA = new("0123456")
		changed, err := b.Report(again)
		require.NoError(t, err, "the same commit, abbreviated, from another ref")
		assert.False(t, changed)
		assert.Equal(t, new("refs/tags/v1.4.0"), b.SourceRef())
		assert.Empty(t, b.PullEvents())
	})

	t.Run("another commit for the same digest is refused", func(t *testing.T) {
		b, err := NewBuild(domBuildProps(), nil)
		require.NoError(t, err)
		_, err = b.Report(BuildDetails{CommitSHA: new("fedcba9876543210")})
		assert.ErrorIs(t, err, ErrBuildConflict)
	})
}

func domReleaseProps() NewReleaseProps {
	return NewReleaseProps{
		OrganizationID: domOrg,
		ApplicationID:  "app_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ChannelID:      "chn_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Version:        "1.4.0",
		Builds: []NewReleaseBuildProps{
			{ArtifactID: "art_01ARZ3NDEKTSV4RRFFQ69G5FB2", BuildID: "bld_01ARZ3NDEKTSV4RRFFQ69G5FB2"},
			{ArtifactID: "art_01ARZ3NDEKTSV4RRFFQ69G5FB1", BuildID: "bld_01ARZ3NDEKTSV4RRFFQ69G5FB1"},
		},
	}
}

func TestNewRelease(t *testing.T) {
	clock.Fix(t, domT0)
	r, err := NewRelease(domReleaseProps(), nil)
	require.NoError(t, err)
	assert.True(t, id.Valid("rel", r.ID()))
	assert.Equal(t, release.StatusDraft, r.Status())
	assert.Equal(t, ReleaseCreated, r.Source())
	assert.Equal(t, "art_01ARZ3NDEKTSV4RRFFQ69G5FB1", r.Builds()[0].ArtifactID, "sorted by artifact")
	assert.Equal(t, []aggregate.Event{ReleaseCreatedEvent{r.Snapshot()}}, r.PullEvents())

	stored, err := NewRelease(domReleaseProps(), new(r.ID()))
	require.NoError(t, err)
	assert.Empty(t, stored.PullEvents())

	for name, edit := range map[string]func(*NewReleaseProps){
		"no builds": func(p *NewReleaseProps) { p.Builds = nil },
		"two builds of one artifact": func(p *NewReleaseProps) {
			p.Builds[1].ArtifactID = p.Builds[0].ArtifactID
		},
		"no version":           func(p *NewReleaseProps) { p.Version = "" },
		"a version with a tab": func(p *NewReleaseProps) { p.Version = "1.4\t0" },
		"an unknown status":    func(p *NewReleaseProps) { p.Status = "deleted" },
		"an unknown source":    func(p *NewReleaseProps) { p.Source = "imported" },
		"no channel":           func(p *NewReleaseProps) { p.ChannelID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			props := domReleaseProps()
			edit(&props)
			_, err := NewRelease(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	t.Run("a version need not be SemVer", func(t *testing.T) {
		props := domReleaseProps()
		props.Version = "build-1234"
		_, err := NewRelease(props, nil)
		require.NoError(t, err)
	})
}

func TestReleaseLifecycle(t *testing.T) {
	clock.Fix(t, domT0)
	r, err := NewRelease(domReleaseProps(), nil)
	require.NoError(t, err)
	r.PullEvents()

	assert.ErrorIs(t, r.Withdraw(), ErrReleaseStatus, "a draft is not withdrawn")
	assert.ErrorIs(t, r.Publish(""), ErrInvalid, "publishing needs its decision")

	clock.Fix(t, domT1)
	require.NoError(t, r.Publish("dcn_1"))
	s := r.Snapshot()
	assert.Equal(t, release.StatusPublished, s.Status)
	assert.Equal(t, &domT1, s.PublishedAt)
	assert.Equal(t, new("dcn_1"), s.PublishDecisionID)
	assert.Equal(t, []aggregate.Event{ReleasePublished{s}}, r.PullEvents())
	assert.ErrorIs(t, r.Publish("dcn_2"), ErrReleaseStatus, "published once")

	clock.Fix(t, domT2)
	require.NoError(t, r.Withdraw())
	s = r.Snapshot()
	assert.Equal(t, release.StatusWithdrawn, s.Status)
	assert.Equal(t, &domT2, s.WithdrawnAt)
	assert.Equal(t, []aggregate.Event{ReleaseWithdrawn{s}}, r.PullEvents())
	assert.ErrorIs(t, r.Withdraw(), ErrReleaseStatus)
}

func TestChannelAdmits(t *testing.T) {
	c := domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"range":"1.0","pre_release":"^$"}`)})
	ok, reasons, err := c.Admits(release.Candidate{Version: "1.4.0"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, reasons)

	ok, reasons, err = c.Admits(release.Candidate{Version: "1.4.0-beta.1"})
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Len(t, reasons, 1)
}

func TestReleaseSnapshotKeys(t *testing.T) {
	keys := func(v any) []string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &m))
		var out []string
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	ts := []string{"created_at", "updated_at"}
	assert.ElementsMatch(t, append([]string{"id", "organization_id", "artifact_kind", "artifact_name", "digest", "provenance", "version",
		"source_id", "commit_sha", "source_ref", "ci_provider", "ci_run_ref", "reported_tags", "built_at"}, ts...), keys(BuildSnapshot{}))
	assert.ElementsMatch(t, append([]string{"id", "organization_id", "application_id", "channel_id", "version", "status", "source",
		"created_by_principal_id", "publish_decision_id", "published_at", "withdrawn_at", "builds"}, ts...), keys(ReleaseSnapshot{}))
	assert.ElementsMatch(t, []string{"artifact_id", "build_id", "created_at"}, keys(ReleaseBuildSnapshot{}))
}
