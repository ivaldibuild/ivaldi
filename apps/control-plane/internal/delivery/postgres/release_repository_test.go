package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

func digest(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func newBuild(org, artifactName, d string, details delivery.BuildDetails, provenance delivery.BuildProvenance) *delivery.Build {
	return must(delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: org, ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: artifactName,
		Digest: d, Provenance: provenance, Details: details,
	}, nil))
}

func TestBuildRepository(t *testing.T) {
	f := newFixture(t)
	builds := NewBuildRepository(f.db)
	at(t, 0)
	built := t0.Add(-time.Hour)

	b := newBuild(orgA, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{
		Version: new("1.4.0"), CommitSHA: new("0123456789abcdef"), SourceRef: new("refs/tags/v1.4.0"),
		CIProvider: new("github_actions"), CIRunRef: new("run/1"), ReportedTags: []string{"v1.4.0", "latest"}, BuiltAt: &built,
	}, delivery.BuildCIReported)
	created, err := builds.CreateBuild(f.ctx, b)
	require.NoError(t, err)
	assert.True(t, created)

	got, err := builds.GetBuild(f.ctx, orgA, b.ID())
	require.NoError(t, err)
	assert.Equal(t, b.Snapshot(), got.Snapshot(), "every field round-trips, tags included")

	t.Run("the same artifact name and digest is not inserted twice", func(t *testing.T) {
		again := newBuild(orgA, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{}, delivery.BuildCIReported)
		created, err := builds.CreateBuild(f.ctx, again)
		require.NoError(t, err)
		assert.False(t, created)
		other := newBuild(orgB, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{}, delivery.BuildCIReported)
		created, err = builds.CreateBuild(f.ctx, other)
		require.NoError(t, err)
		assert.True(t, created, "another Organization's Builds are its own")
	})

	t.Run("finds a build by digest, locked", func(t *testing.T) {
		got, err := builds.GetBuildByDigestForUpdate(f.ctx, orgA, "ghcr.io/acme/api", digest("a"))
		require.NoError(t, err)
		assert.Equal(t, b.ID(), got.ID())
		_, err = builds.GetBuildByDigestForUpdate(f.ctx, orgA, "ghcr.io/acme/web", digest("a"))
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("an observed build is upgraded", func(t *testing.T) {
		seen := newBuild(orgA, "ghcr.io/acme/web", digest("b"), delivery.BuildDetails{}, delivery.BuildObserved)
		_, err := builds.CreateBuild(f.ctx, seen)
		require.NoError(t, err)
		at(t, time.Minute)
		_, err = seen.Report(delivery.BuildDetails{CommitSHA: new("fedcba98"), SourceRef: new("refs/heads/main")})
		require.NoError(t, err)
		require.NoError(t, builds.UpdateBuild(f.ctx, seen))
		got, err := builds.GetBuild(f.ctx, orgA, seen.ID())
		require.NoError(t, err)
		assert.Equal(t, seen.Snapshot(), got.Snapshot())

		listed, err := builds.GetBuilds(f.ctx, orgA, []string{b.ID(), seen.ID(), "bld_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{b.ID(), seen.ID()}, idsOf(listed))
	})
}

func TestReleaseRepository(t *testing.T) {
	f := newFixture(t)
	builds := NewBuildRepository(f.db)
	releases := NewReleaseRepository(f.db)
	at(t, 0)

	env := f.createEnv(t, orgA, wsA, "prod")
	lc := f.createLifecycle(t, orgA, wsA, "standard", phase("Prod", env.ID()))
	app := f.createApp(t, orgA, wsA, projA, "api")
	api := f.createArtifact(t, orgA, app.ID(), "api")
	chart := f.createArtifact(t, orgA, app.ID(), "chart")
	main := f.createChannel(t, orgA, app.ID(), lc.ID(), "main", true)
	beta := f.createChannel(t, orgA, app.ID(), lc.ID(), "beta", false)

	build := func(c string) *delivery.Build {
		b := newBuild(orgA, "ghcr.io/acme/api", digest(c), delivery.BuildDetails{}, delivery.BuildCIReported)
		_, err := builds.CreateBuild(f.ctx, b)
		require.NoError(t, err)
		return b
	}
	newRelease := func(version, channelID string, b ...*delivery.Build) *delivery.Release {
		props := delivery.NewReleaseProps{OrganizationID: orgA, ApplicationID: app.ID(), ChannelID: channelID, Version: version,
			CreatedByPrincipalID: new("prn_ada")}
		artifacts := []string{api.ID(), chart.ID()}
		for i, bb := range b {
			props.Builds = append(props.Builds, delivery.NewReleaseBuildProps{ArtifactID: artifacts[i], BuildID: bb.ID()})
		}
		return must(delivery.NewRelease(props, nil))
	}

	r1 := newRelease("1.0.0", main.ID(), build("1"), build("2"))
	require.NoError(t, releases.CreateRelease(f.ctx, r1))
	got, err := releases.GetRelease(f.ctx, orgA, r1.ID())
	require.NoError(t, err)
	assert.Equal(t, r1.Snapshot(), got.Snapshot(), "with its builds")

	t.Run("a version is unique within the application", func(t *testing.T) {
		err := releases.CreateRelease(f.ctx, newRelease("1.0.0", beta.ID(), build("3")))
		assert.ErrorIs(t, err, delivery.ErrAlreadyExists)
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM delivery.releases"), "nothing half-written")
	})

	t.Run("a channel of another application is refused", func(t *testing.T) {
		other := f.createApp(t, orgA, wsA, projA, "web")
		otherChannel := f.createChannel(t, orgA, other.ID(), lc.ID(), "main", true)
		err := releases.CreateRelease(f.ctx, newRelease("9.0.0", otherChannel.ID(), build("4")))
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})

	at(t, time.Minute)
	r2 := newRelease("1.1.0-beta.1", beta.ID(), build("5"))
	require.NoError(t, releases.CreateRelease(f.ctx, r2))
	at(t, 2*time.Minute)
	r3 := newRelease("1.1.0", main.ID(), build("6"))
	require.NoError(t, releases.CreateRelease(f.ctx, r3))

	t.Run("publishes and withdraws", func(t *testing.T) {
		locked, err := releases.GetReleaseForUpdate(f.ctx, orgA, r3.ID())
		require.NoError(t, err)
		at(t, 3*time.Minute)
		require.NoError(t, locked.Publish("dcn_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
		require.NoError(t, releases.UpdateRelease(f.ctx, locked))
		got, err := releases.GetRelease(f.ctx, orgA, r3.ID())
		require.NoError(t, err)
		assert.Equal(t, locked.Snapshot(), got.Snapshot())
		assert.Equal(t, release.StatusPublished, got.Status())
	})

	t.Run("lists by channel and status, newest first", func(t *testing.T) {
		list := func(q delivery.ListReleasesQuery) []string {
			t.Helper()
			q.OrganizationID, q.ApplicationID = orgA, app.ID()
			page, err := releases.ListReleases(f.ctx, q)
			require.NoError(t, err)
			return idsOf(page.Items)
		}
		desc := pagination.Query{SortOrder: pagination.SortDescending}
		assert.Equal(t, []string{r3.ID(), r2.ID(), r1.ID()}, list(delivery.ListReleasesQuery{Page: desc}))
		assert.Equal(t, []string{r3.ID(), r1.ID()}, list(delivery.ListReleasesQuery{ChannelID: main.ID(), Page: desc}))
		assert.Equal(t, []string{r3.ID()}, list(delivery.ListReleasesQuery{Status: release.StatusPublished}))
		assert.Equal(t, []string{r1.ID(), r3.ID(), r2.ID()}, list(delivery.ListReleasesQuery{Page: pagination.Query{SortBy: "version"}}), "versions sort as text")

		pages, _ := walk(t, pagination.Query{Limit: 2}, func(q pagination.Query) (*pagination.Result[*delivery.Release], error) {
			return releases.ListReleases(f.ctx, delivery.ListReleasesQuery{OrganizationID: orgA, ApplicationID: app.ID(), Page: q})
		})
		assert.Equal(t, []string{r1.ID(), r2.ID(), r3.ID()}, flatten(pages))

		_, err := releases.ListReleases(f.ctx, delivery.ListReleasesQuery{OrganizationID: orgA, ApplicationID: app.ID(), Page: pagination.Query{SortBy: "status"}})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		_, err := releases.GetRelease(f.ctx, orgB, r1.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})
}
