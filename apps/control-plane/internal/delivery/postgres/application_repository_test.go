package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

func TestApplicationRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		app := must(delivery.NewApplication(delivery.NewApplicationProps{
			OrganizationID:   orgA,
			WorkspaceID:      wsA,
			ProjectID:        projA,
			OwnerTeamID:      teamA,
			Slug:             "api",
			DisplayName:      "API",
			SourceID:         new("src_api"),
			WorkloadSelector: map[string]string{"app.kubernetes.io/name": "api", "tier": "backend"},
		}, nil))
		require.NoError(t, f.apps.CreateApplication(ctx, app))

		got, err := f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())

		bySlug, err := f.apps.GetApplicationBySlug(ctx, orgA, projA, "api")
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip with the default selector", func(t *testing.T) {
		app := f.createApp(t, orgA, wsA, projA, "worker")
		got, err := f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())
		assert.Equal(t, map[string]string{delivery.WorkloadSelectorLabel: app.ID()}, got.WorkloadSelector())
		assert.Nil(t, got.SourceID())
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		app := f.createApp(t, orgA, wsA, projA, "isolated")
		_, err := f.apps.GetApplication(ctx, orgB, app.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.apps.GetApplicationForUpdate(ctx, orgB, app.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.apps.GetApplicationBySlug(ctx, orgB, projA, "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		page, err := f.apps.ListApplications(ctx, delivery.ListApplicationsQuery{OrganizationID: orgB, ProjectID: projA})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		n, err := f.apps.CountActiveApplications(ctx, orgB, projA)
		require.NoError(t, err)
		assert.Zero(t, n)
		f.createApp(t, orgB, wsA, projA, "isolated")
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		app := f.createApp(t, orgA, wsA, projA, "to-update")

		at(t, time.Minute)
		require.NoError(t, app.Update(delivery.UpdateApplicationProps{
			Slug:             new("updated"),
			DisplayName:      new("Updated"),
			WorkloadSelector: map[string]string{"app": "updated"},
		}))
		require.NoError(t, app.LinkSource("src_updated"))
		require.NoError(t, app.TransferTo(teamB))
		require.NoError(t, f.apps.UpdateApplication(ctx, app))
		got, err := f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())

		at(t, 2*time.Minute)
		require.NoError(t, app.UnlinkSource())
		require.NoError(t, app.Update(delivery.UpdateApplicationProps{WorkloadSelector: map[string]string{}}))
		require.NoError(t, f.apps.UpdateApplication(ctx, app))
		got, err = f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())
		assert.Nil(t, got.SourceID())
		assert.Equal(t, map[string]string{delivery.WorkloadSelectorLabel: app.ID()}, got.WorkloadSelector())

		at(t, 3*time.Minute)
		app.Archive()
		require.NoError(t, f.apps.UpdateApplication(ctx, app))
		got, err = f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())
		_, err = f.apps.GetApplicationBySlug(ctx, orgA, projA, "updated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)

		at(t, 4*time.Minute)
		app.Restore()
		require.NoError(t, f.apps.UpdateApplication(ctx, app))
		got, err = f.apps.GetApplication(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.UpdateApplication(ctx, newApp(orgA, wsA, projA, teamA, "never-stored")), delivery.ErrNotFound)
	})

	t.Run("slugs are unique among a project's active applications", func(t *testing.T) {
		first := f.createApp(t, orgA, wsA, projA, "clash")
		assert.ErrorIs(t, f.apps.CreateApplication(ctx, newApp(orgA, wsA, projA, teamA, "clash")), delivery.ErrSlugTaken)
		f.createApp(t, orgA, wsA, projB, "clash")

		first.Archive()
		require.NoError(t, f.apps.UpdateApplication(ctx, first))
		second := f.createApp(t, orgA, wsA, projA, "clash")
		got, err := f.apps.GetApplicationBySlug(ctx, orgA, projA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.apps.UpdateApplication(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("by ID or slug", func(t *testing.T) {
		app := f.createApp(t, orgA, wsA, projA, "named")
		for _, identifier := range []string{app.ID(), "named"} {
			got, err := f.apps.GetApplicationByIdentifier(ctx, orgA, identifier)
			require.NoError(t, err, identifier)
			assert.Equal(t, app.Snapshot(), got.Snapshot(), identifier)
		}
		_, err := f.apps.GetApplicationByIdentifier(ctx, orgB, "named")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "another Organization's slug")
		_, err = f.apps.GetApplicationByIdentifier(ctx, orgA, "nothing")
		assert.ErrorIs(t, err, delivery.ErrNotFound)

		other := f.createApp(t, orgA, wsA, projB, "named")
		_, err = f.apps.GetApplicationByIdentifier(ctx, orgA, "named")
		assert.ErrorIs(t, err, delivery.ErrAmbiguous, "two Projects use the slug")
		got, err := f.apps.GetApplicationByIdentifier(ctx, orgA, other.ID())
		require.NoError(t, err)
		assert.Equal(t, other.ID(), got.ID(), "an ID is never ambiguous")

		other.Archive()
		require.NoError(t, f.apps.UpdateApplication(ctx, other))
		got, err = f.apps.GetApplicationByIdentifier(ctx, orgA, "named")
		require.NoError(t, err)
		assert.Equal(t, app.ID(), got.ID(), "archived Applications' slugs name nothing")
		got, err = f.apps.GetApplicationByIdentifier(ctx, orgA, other.ID())
		require.NoError(t, err)
		assert.Equal(t, other.ID(), got.ID(), "an archived Application is found by its ID")
	})
}

func TestApplicationListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	a1 := f.createApp(t, orgA, wsA, projA, "a1")
	at(t, time.Second)
	a2 := newApp(orgA, wsA, projA, teamB, "a2")
	require.NoError(t, f.apps.CreateApplication(ctx, a2))
	at(t, 2*time.Second)
	b1 := f.createApp(t, orgA, wsA, projB, "b1")
	at(t, 3*time.Second)
	archived := f.createApp(t, orgA, wsA, projA, "archived")
	archived.Archive()
	require.NoError(t, f.apps.UpdateApplication(ctx, archived))
	f.createApp(t, orgA, wsB, "prj_c", "elsewhere")
	f.createApp(t, orgB, wsA, projA, "other-org")

	list := func(q delivery.ListApplicationsQuery) []string {
		t.Helper()
		q.OrganizationID = orgA
		page, err := f.apps.ListApplications(ctx, q)
		require.NoError(t, err)
		assert.Equal(t, len(page.Items), page.TotalCount)
		return idsOf(page.Items)
	}
	assert.Equal(t, []string{a1.ID(), a2.ID()}, list(delivery.ListApplicationsQuery{ProjectID: projA}))
	assert.Equal(t, []string{a1.ID(), a2.ID(), archived.ID()}, list(delivery.ListApplicationsQuery{ProjectID: projA, IncludeArchived: true}))
	assert.Equal(t, []string{a1.ID(), a2.ID(), b1.ID()}, list(delivery.ListApplicationsQuery{WorkspaceID: wsA}))
	assert.Equal(t, []string{a2.ID()}, list(delivery.ListApplicationsQuery{WorkspaceID: wsA, OwnerTeamID: teamB}))
	assert.Equal(t, []string{a1.ID(), b1.ID()}, list(delivery.ListApplicationsQuery{WorkspaceID: wsA, OwnerTeamID: teamA}))
	assert.Empty(t, list(delivery.ListApplicationsQuery{WorkspaceID: wsB, ProjectID: projA}), "a project outside the workspace matches nothing")

	page, err := f.apps.ListApplications(ctx, delivery.ListApplicationsQuery{OrganizationID: orgA, ProjectID: projA, Page: pagination.Query{Limit: 1}})
	require.NoError(t, err)
	assert.Equal(t, a1.Snapshot(), page.Items[0].Snapshot())
	assert.True(t, page.HasNext)
	assert.Equal(t, 2, page.TotalCount)

	_, err = f.apps.ListApplications(ctx, delivery.ListApplicationsQuery{OrganizationID: orgA, OwnerTeamID: teamA})
	assert.ErrorIs(t, err, delivery.ErrInvalid, "a list needs a project or a workspace")

	n, err := f.apps.CountActiveApplications(ctx, orgA, projA)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = f.apps.CountActiveApplications(ctx, orgA, projB)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = f.apps.CountActiveApplicationsOwnedBy(ctx, orgA, teamA)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "a1, b1, and elsewhere; not the archived one or another organization's")
	n, err = f.apps.CountActiveApplicationsOwnedBy(ctx, orgA, teamB)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestApplicationForUpdate(t *testing.T) {
	f := newFixture(t)
	app := f.createApp(t, orgA, wsA, projA, "locked")

	at(t, time.Minute)
	err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
		got, err := f.apps.GetApplicationForUpdate(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), got.Snapshot())
		f.assertLocked(t, "applications", app.ID())
		require.NoError(t, got.TransferTo(teamB))
		app = got
		return f.apps.UpdateApplication(ctx, got)
	})
	require.NoError(t, err)
	f.assertNotLocked(t, "applications", app.ID())
	got := must(f.apps.GetApplication(f.ctx, orgA, app.ID()))
	assert.Equal(t, app.Snapshot(), got.Snapshot())

	stop := errors.New("stop")
	err = f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
		got := must(f.apps.GetApplicationForUpdate(ctx, orgA, app.ID()))
		require.NoError(t, got.TransferTo(teamA))
		require.NoError(t, f.apps.UpdateApplication(ctx, got))
		return stop
	})
	require.ErrorIs(t, err, stop)
	assert.Equal(t, teamB, must(f.apps.GetApplication(f.ctx, orgA, app.ID())).OwnerTeamID(), "the rollback discarded the transfer")
}

func TestArtifactRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	app := f.createApp(t, orgA, wsA, projA, "api")
	reg := f.createRegistry(t, orgA, wsA, "ghcr")

	t.Run("round trip", func(t *testing.T) {
		art := must(delivery.NewArtifact(delivery.NewArtifactProps{
			OrganizationID: orgA,
			ApplicationID:  app.ID(),
			Name:           "chart",
			ArtifactKind:   delivery.ArtifactHelmChart,
			ArtifactName:   "ghcr.io/acme/charts/api",
			RegistryID:     new(reg.ID()),
			Required:       false,
			Source:         delivery.ArtifactObserved,
		}, nil))
		require.NoError(t, f.apps.CreateArtifact(ctx, art))

		got, err := f.apps.GetArtifact(ctx, orgA, art.ID())
		require.NoError(t, err)
		assert.Equal(t, art.Snapshot(), got.Snapshot())
		assert.False(t, got.Required(), "false is stored, not the column default")
	})

	t.Run("unknown parents are not found", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.CreateArtifact(ctx, newArtifact(orgA, id.New("app"), "orphan")), delivery.ErrNotFound)

		art := newArtifact(orgA, app.ID(), "no-registry")
		require.NoError(t, art.UseRegistry(id.New("reg")))
		assert.ErrorIs(t, f.apps.CreateArtifact(ctx, art), delivery.ErrNotFound)

		foreign := f.createRegistry(t, orgB, wsA, "foreign")
		art = newArtifact(orgA, app.ID(), "foreign-registry")
		require.NoError(t, art.UseRegistry(foreign.ID()))
		assert.ErrorIs(t, f.apps.CreateArtifact(ctx, art), delivery.ErrNotFound)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		art := f.createArtifact(t, orgA, app.ID(), "isolated")
		_, err := f.apps.GetArtifact(ctx, orgB, art.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		list, err := f.apps.ListArtifacts(ctx, orgB, app.ID(), true)
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		art := must(delivery.NewArtifact(delivery.NewArtifactProps{
			OrganizationID: orgA, ApplicationID: app.ID(), Name: "migrate", ArtifactKind: delivery.ArtifactContainerImage,
			ArtifactName: "ghcr.io/acme/migrate", Source: delivery.ArtifactObserved,
		}, nil))
		require.NoError(t, f.apps.CreateArtifact(ctx, art))

		at(t, time.Minute)
		require.NoError(t, art.Update(delivery.UpdateArtifactProps{Name: new("migrations"), ArtifactName: new("ghcr.io/acme/migrations"), Required: new(true)}))
		require.NoError(t, art.UseRegistry(reg.ID()))
		require.NoError(t, art.Declare())
		require.NoError(t, f.apps.UpdateArtifact(ctx, art))
		got, err := f.apps.GetArtifact(ctx, orgA, art.ID())
		require.NoError(t, err)
		assert.Equal(t, art.Snapshot(), got.Snapshot())

		at(t, 2*time.Minute)
		require.NoError(t, art.ClearRegistry())
		art.Archive()
		require.NoError(t, f.apps.UpdateArtifact(ctx, art))
		got, err = f.apps.GetArtifact(ctx, orgA, art.ID())
		require.NoError(t, err)
		assert.Equal(t, art.Snapshot(), got.Snapshot())
		assert.Nil(t, got.RegistryID())

		at(t, 3*time.Minute)
		art.Restore()
		require.NoError(t, f.apps.UpdateArtifact(ctx, art))
		got, err = f.apps.GetArtifact(ctx, orgA, art.ID())
		require.NoError(t, err)
		assert.Equal(t, art.Snapshot(), got.Snapshot())

		missing := newArtifact(orgA, app.ID(), "never-stored")
		assert.ErrorIs(t, f.apps.UpdateArtifact(ctx, missing), delivery.ErrNotFound)

		require.NoError(t, art.UseRegistry(id.New("reg")))
		assert.ErrorIs(t, f.apps.UpdateArtifact(ctx, art), delivery.ErrNotFound, "an unknown registry")
	})

	t.Run("names are unique among an application's active artifacts", func(t *testing.T) {
		first := f.createArtifact(t, orgA, app.ID(), "clash")
		assert.ErrorIs(t, f.apps.CreateArtifact(ctx, newArtifact(orgA, app.ID(), "clash")), delivery.ErrSlugTaken)
		other := f.createApp(t, orgA, wsA, projA, "other")
		f.createArtifact(t, orgA, other.ID(), "clash")

		first.Archive()
		require.NoError(t, f.apps.UpdateArtifact(ctx, first))
		f.createArtifact(t, orgA, app.ID(), "clash")
		first.Restore()
		assert.ErrorIs(t, f.apps.UpdateArtifact(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("list", func(t *testing.T) {
		other := f.createApp(t, orgA, wsA, projA, "listed")
		web := f.createArtifact(t, orgA, other.ID(), "web")
		api := f.createArtifact(t, orgA, other.ID(), "api")
		old := f.createArtifact(t, orgA, other.ID(), "old")
		old.Archive()
		require.NoError(t, f.apps.UpdateArtifact(ctx, old))

		list, err := f.apps.ListArtifacts(ctx, orgA, other.ID(), false)
		require.NoError(t, err)
		assert.Equal(t, []string{api.ID(), web.ID()}, idsOf(list), "by name")
		assert.Equal(t, api.Snapshot(), list[0].Snapshot())

		list, err = f.apps.ListArtifacts(ctx, orgA, other.ID(), true)
		require.NoError(t, err)
		assert.Equal(t, []string{api.ID(), old.ID(), web.ID()}, idsOf(list))
	})
}

func versionRule(config string) delivery.NewChannelRuleProps {
	return delivery.NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(config)}
}

func TestChannelRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "staging")
	lfc := f.createLifecycle(t, orgA, wsA, "mainline", phase("Staging", env.ID()))
	otherLfc := f.createLifecycle(t, orgA, wsA, "hotfix")
	app := f.createApp(t, orgA, wsA, projA, "api")
	art := f.createArtifact(t, orgA, app.ID(), "api")

	t.Run("round trip", func(t *testing.T) {
		scoped := gitRefRule("refs/heads/main", "refs/heads/release/**")
		scoped.ArtifactID = new(art.ID())
		c := must(delivery.NewChannel(delivery.NewChannelProps{
			OrganizationID: orgA,
			ApplicationID:  app.ID(),
			LifecycleID:    lfc.ID(),
			Slug:           "default",
			DisplayName:    "Default",
			Description:    new("Everything"),
			IsDefault:      true,
			Rules: []delivery.NewChannelRuleProps{
				// Whitespace and key order differ from the canonical form.
				versionRule(`{ "pre_release": "^$",   "range": "[1.0,2.0)" }`),
				scoped,
				versionRule(`{"pattern":"^build-\\d+$"}`),
			},
		}, nil))
		require.NoError(t, f.apps.CreateChannel(ctx, c))

		got, err := f.apps.GetChannel(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		rules := got.Rules()
		require.Len(t, rules, 3)
		assert.Equal(t, idsOfRules(c.Rules()), idsOfRules(rules), "rules come back in creation order")
		assert.JSONEq(t, `{"range":"[1.0,2.0)","pre_release":"^$"}`, string(rules[0].Config))
		assert.Equal(t, `{"range":"[1.0,2.0)","pre_release":"^$"}`, string(rules[0].Config), "canonical JSON")
		assert.Equal(t, new(art.ID()), rules[1].ArtifactID)

		def, err := f.apps.GetDefaultChannel(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), def.Snapshot())
	})

	t.Run("round trip without rules", func(t *testing.T) {
		c := f.createChannel(t, orgA, app.ID(), lfc.ID(), "bare", false)
		got, err := f.apps.GetChannel(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.Empty(t, got.Rules())
	})

	t.Run("a second default conflicts", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, newChannel(orgA, app.ID(), lfc.ID(), "second-default", true)), delivery.ErrConflict)
	})

	t.Run("unknown parents are not found and nothing is stored", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, newChannel(orgA, app.ID(), id.New("lfc"), "no-lifecycle", false)), delivery.ErrNotFound)
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, newChannel(orgA, id.New("app"), lfc.ID(), "no-app", false)), delivery.ErrNotFound)

		bad := gitRefRule("refs/heads/main")
		bad.ArtifactID = new(id.New("art"))
		c := newChannel(orgA, app.ID(), lfc.ID(), "bad-rule", false, gitRefRule("refs/tags/*"), bad)
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, c), delivery.ErrNotFound)
		_, err := f.apps.GetChannel(ctx, orgA, c.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound, "the channel rolled back with its rules")

		// An artifact of another application of the same organization.
		other := f.createApp(t, orgA, wsA, projA, "other")
		otherArt := f.createArtifact(t, orgA, other.ID(), "api")
		cross := gitRefRule("refs/heads/main")
		cross.ArtifactID = new(otherArt.ID())
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, newChannel(orgA, app.ID(), lfc.ID(), "cross-app", false, cross)), delivery.ErrNotFound)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		c := f.createChannel(t, orgA, app.ID(), lfc.ID(), "isolated", false, gitRefRule("refs/heads/main"))
		_, err := f.apps.GetChannel(ctx, orgB, c.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.apps.GetDefaultChannel(ctx, orgB, app.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		list, err := f.apps.ListChannels(ctx, orgB, app.ID(), true)
		require.NoError(t, err)
		assert.Empty(t, list)
		n, err := f.apps.CountActiveChannelsWithLifecycle(ctx, orgB, lfc.ID())
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("slugs are unique among an application's active channels", func(t *testing.T) {
		first := f.createChannel(t, orgA, app.ID(), lfc.ID(), "clash", false)
		assert.ErrorIs(t, f.apps.CreateChannel(ctx, newChannel(orgA, app.ID(), lfc.ID(), "clash", false)), delivery.ErrSlugTaken)

		require.NoError(t, first.Archive())
		require.NoError(t, f.apps.UpdateChannel(ctx, first))
		f.createChannel(t, orgA, app.ID(), lfc.ID(), "clash", false)
		first.Restore()
		assert.ErrorIs(t, f.apps.UpdateChannel(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.UpdateChannel(ctx, newChannel(orgA, app.ID(), lfc.ID(), "never-stored", false)), delivery.ErrNotFound)
	})

	t.Run("update adds, changes, and removes rules", func(t *testing.T) {
		c := f.createChannel(t, orgA, app.ID(), lfc.ID(), "rules", false,
			gitRefRule("refs/heads/main"),
			versionRule(`{"range":"[1.0,2.0)"}`),
			gitRefRule("refs/tags/*"),
		)
		before := c.Rules()

		at(t, time.Minute)
		require.NoError(t, c.Update(delivery.UpdateChannelProps{DisplayName: new("Rules"), Description: new("Changed")}))
		require.NoError(t, c.UseLifecycle(otherLfc.ID()))
		require.NoError(t, c.RemoveRule(before[0].ID))
		require.NoError(t, c.UpdateRule(before[1].ID, delivery.ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: new(art.ID()), Config: json.RawMessage(`{"patterns":["refs/heads/hotfix/**"]}`)}))
		added, err := c.AddRule(delivery.ChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)})
		require.NoError(t, err)
		require.NoError(t, f.apps.UpdateChannel(ctx, c))

		got, err := f.apps.GetChannel(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		rules := got.Rules()
		assert.Equal(t, []string{before[1].ID, before[2].ID, added.ID}, idsOfRules(rules))
		assert.Equal(t, before[1].CreatedAt, rules[0].CreatedAt, "a changed rule keeps its creation time")
		assert.Equal(t, t0.Add(time.Minute), rules[0].UpdatedAt)
		assert.Equal(t, release.RuleGitRef, rules[0].Kind)
		assert.Equal(t, otherLfc.ID(), got.LifecycleID())
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.channel_rules WHERE id = ?", before[0].ID))

		at(t, 2*time.Minute)
		for _, r := range c.Rules() {
			require.NoError(t, c.RemoveRule(r.ID))
		}
		require.NoError(t, f.apps.UpdateChannel(ctx, c))
		got, err = f.apps.GetChannel(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.channel_rules WHERE channel_id = ?", c.ID()))
	})

	t.Run("a failed update changes nothing", func(t *testing.T) {
		c := f.createChannel(t, orgA, app.ID(), lfc.ID(), "atomic", false, gitRefRule("refs/heads/main"))
		before := must(f.apps.GetChannel(ctx, orgA, c.ID()))

		at(t, 3*time.Minute)
		require.NoError(t, c.Update(delivery.UpdateChannelProps{DisplayName: new("Broken")}))
		require.NoError(t, c.RemoveRule(c.Rules()[0].ID))
		_, err := c.AddRule(delivery.ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: new(id.New("art")), Config: json.RawMessage(`{"patterns":["refs/heads/x"]}`)})
		require.NoError(t, err)
		assert.ErrorIs(t, f.apps.UpdateChannel(ctx, c), delivery.ErrNotFound)

		got := must(f.apps.GetChannel(ctx, orgA, c.ID()))
		assert.Equal(t, before.Snapshot(), got.Snapshot())
	})

	t.Run("move the default inside a transaction", func(t *testing.T) {
		next := f.createChannel(t, orgA, app.ID(), lfc.ID(), "next", false)

		at(t, 4*time.Minute)
		err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
			old, err := f.apps.GetDefaultChannel(ctx, orgA, app.ID())
			require.NoError(t, err)

			// Setting the new default first clashes with the old one.
			early := must(f.apps.GetChannel(ctx, orgA, next.ID()))
			require.NoError(t, early.MakeDefault())
			require.ErrorIs(t, f.apps.UpdateChannel(ctx, early), delivery.ErrConflict)

			old.ClearDefault()
			if err := f.apps.UpdateChannel(ctx, old); err != nil {
				return err
			}
			require.NoError(t, next.MakeDefault())
			return f.apps.UpdateChannel(ctx, next)
		})
		require.NoError(t, err)

		def, err := f.apps.GetDefaultChannel(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, next.Snapshot(), def.Snapshot())
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM delivery.channels WHERE application_id = ? AND is_default", app.ID()))
	})

	t.Run("a rolled back default move changes nothing", func(t *testing.T) {
		stop := errors.New("stop")
		err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
			old := must(f.apps.GetDefaultChannel(ctx, orgA, app.ID()))
			old.ClearDefault()
			require.NoError(t, f.apps.UpdateChannel(ctx, old))
			return stop
		})
		require.ErrorIs(t, err, stop)
		def, err := f.apps.GetDefaultChannel(ctx, orgA, app.ID())
		require.NoError(t, err)
		assert.Equal(t, "next", def.Slug())
	})
}

func idsOfRules(rules []delivery.ChannelRuleSnapshot) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.ID
	}
	return out
}

func TestChannelListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	lfc := f.createLifecycle(t, orgA, wsA, "mainline")
	otherLfc := f.createLifecycle(t, orgA, wsA, "hotfix")
	app := f.createApp(t, orgA, wsA, projA, "api")
	other := f.createApp(t, orgA, wsA, projA, "worker")

	zeta := f.createChannel(t, orgA, app.ID(), lfc.ID(), "zeta", true, gitRefRule("refs/heads/main"))
	beta := f.createChannel(t, orgA, app.ID(), lfc.ID(), "beta", false)
	alpha := f.createChannel(t, orgA, app.ID(), otherLfc.ID(), "alpha", false, gitRefRule("refs/tags/*"))
	archived := f.createChannel(t, orgA, app.ID(), lfc.ID(), "archived", false)
	require.NoError(t, archived.Archive())
	require.NoError(t, f.apps.UpdateChannel(ctx, archived))
	f.createChannel(t, orgA, other.ID(), lfc.ID(), "default", true)

	list, err := f.apps.ListChannels(ctx, orgA, app.ID(), false)
	require.NoError(t, err)
	assert.Equal(t, []string{zeta.ID(), alpha.ID(), beta.ID()}, idsOf(list), "the default first, then by slug")
	assert.Equal(t, zeta.Snapshot(), list[0].Snapshot(), "listed channels carry their rules")
	assert.Equal(t, alpha.Snapshot(), list[1].Snapshot())

	list, err = f.apps.ListChannels(ctx, orgA, app.ID(), true)
	require.NoError(t, err)
	assert.Equal(t, []string{zeta.ID(), alpha.ID(), archived.ID(), beta.ID()}, idsOf(list))

	n, err := f.apps.CountActiveChannelsWithLifecycle(ctx, orgA, lfc.ID())
	require.NoError(t, err)
	assert.Equal(t, 3, n, "zeta, beta, and the other application's default; not the archived one")
	n, err = f.apps.CountActiveChannelsWithLifecycle(ctx, orgA, otherLfc.ID())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// placementFixture is an application with two targets.
func placementFixture(t *testing.T) (*fixture, *delivery.Application, *delivery.Environment, *delivery.Target, *delivery.Target) {
	f := newFixture(t)
	env := f.createEnv(t, orgA, wsA, "staging")
	app := f.createApp(t, orgA, wsA, projA, "api")
	return f, app, env, f.createTarget(t, orgA, env.ID(), "one"), f.createTarget(t, orgA, env.ID(), "two")
}

func TestPlacementRepository(t *testing.T) {
	f, app, env, one, two := placementFixture(t)
	ctx := f.ctx

	configured := newPlacement(orgA, app.ID(), one.ID(), &delivery.ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_values_a", "cred_values_b"}})
	require.NoError(t, f.apps.CreatePlacement(ctx, configured))
	at(t, time.Second)
	tracked := must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: orgA, ApplicationID: app.ID(), TargetID: two.ID(), Source: delivery.PlacementObserved,
	}))
	require.NoError(t, f.apps.CreatePlacement(ctx, tracked))

	t.Run("round trip", func(t *testing.T) {
		got, err := f.apps.GetPlacement(ctx, orgA, app.ID(), one.ID())
		require.NoError(t, err)
		assert.Equal(t, configured.Snapshot(), got.Snapshot())

		got, err = f.apps.GetPlacement(ctx, orgA, app.ID(), two.ID())
		require.NoError(t, err)
		assert.Equal(t, tracked.Snapshot(), got.Snapshot())
		assert.Nil(t, got.ApplyConfig(), "no apply config stays nil")
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM delivery.application_targets WHERE target_id = ? AND apply_config IS NULL", two.ID()), "and is stored as NULL")
	})

	t.Run("create if absent leaves the transaction usable", func(t *testing.T) {
		err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
			again := must(delivery.NewPlacement(delivery.NewPlacementProps{
				OrganizationID: orgA, ApplicationID: app.ID(), TargetID: two.ID(), Source: delivery.PlacementObserved,
			}))
			inserted, err := f.apps.CreatePlacementIfAbsent(ctx, again)
			require.NoError(t, err)
			assert.False(t, inserted, "the placement exists")

			got, err := f.apps.GetPlacement(ctx, orgA, app.ID(), two.ID())
			require.NoError(t, err, "the transaction is still usable")
			assert.Equal(t, tracked.Snapshot(), got.Snapshot(), "the existing placement is kept")
			return nil
		})
		require.NoError(t, err)

		list, err := f.apps.ListPlacementsByTarget(ctx, orgA, two.ID())
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, tracked.Snapshot(), list[0].Snapshot())
	})

	t.Run("list", func(t *testing.T) {
		list, err := f.apps.ListPlacements(ctx, orgA, app.ID())
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, configured.Snapshot(), list[0].Snapshot())
		assert.Equal(t, tracked.Snapshot(), list[1].Snapshot())

		list, err = f.apps.ListPlacements(ctx, orgB, app.ID())
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		_, err := f.apps.GetPlacement(ctx, orgB, app.ID(), one.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		assert.ErrorIs(t, f.apps.DeletePlacement(ctx, orgB, app.ID(), one.ID()), delivery.ErrNotFound)
		_, err = f.apps.GetPlacement(ctx, orgA, app.ID(), one.ID())
		assert.NoError(t, err, "the other organization's delete removed nothing")
	})

	t.Run("a duplicate already exists", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, app.ID(), one.ID(), nil)), delivery.ErrAlreadyExists)
	})

	t.Run("unknown parents are not found", func(t *testing.T) {
		assert.ErrorIs(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, app.ID(), id.New("tgt"), nil)), delivery.ErrNotFound)
		assert.ErrorIs(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, id.New("app"), one.ID(), nil)), delivery.ErrNotFound)
		foreignEnv := f.createEnv(t, orgB, wsA, "foreign")
		foreign := f.createTarget(t, orgB, foreignEnv.ID(), "foreign")
		assert.ErrorIs(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, app.ID(), foreign.ID(), nil)), delivery.ErrNotFound)
	})

	t.Run("update writes the configuration", func(t *testing.T) {
		at(t, time.Minute)
		require.NoError(t, tracked.Configure(delivery.ApplyConfig{DefinitionArtifact: "manifests"}))
		require.NoError(t, f.apps.UpdatePlacement(ctx, tracked))
		got, err := f.apps.GetPlacement(ctx, orgA, app.ID(), two.ID())
		require.NoError(t, err)
		assert.Equal(t, tracked.Snapshot(), got.Snapshot())
		assert.Equal(t, delivery.PlacementConfigured, got.Source())

		at(t, 2*time.Minute)
		tracked.ClearApplyConfig()
		require.NoError(t, f.apps.UpdatePlacement(ctx, tracked))
		got, err = f.apps.GetPlacement(ctx, orgA, app.ID(), two.ID())
		require.NoError(t, err)
		assert.Equal(t, tracked.Snapshot(), got.Snapshot())
		assert.Nil(t, got.ApplyConfig(), "clearing stores NULL")
		assert.Nil(t, got.CurrentDeploymentID())
		assert.Nil(t, got.CurrentReleaseID())

		assert.ErrorIs(t, f.apps.UpdatePlacement(ctx, newPlacement(orgA, app.ID(), id.New("tgt"), nil)), delivery.ErrNotFound)
	})

	t.Run("update leaves the current deployment and release alone", func(t *testing.T) {
		lfc := f.createLifecycle(t, orgA, wsA, "mainline", phase("Staging", env.ID()))
		ch := f.createChannel(t, orgA, app.ID(), lfc.ID(), "default", true)
		releaseID, deploymentID := "rel_"+id.ULID(), "dep_"+id.ULID()
		require.NoError(t, f.db.Exec(`INSERT INTO delivery.releases (id, organization_id, application_id, channel_id, version, status, source)
			VALUES (?, ?, ?, ?, '1.0.0', 'published', 'manual')`, releaseID, orgA, app.ID(), ch.ID()).Error)
		require.NoError(t, f.db.Exec(`INSERT INTO delivery.deployments (id, organization_id, application_id, release_id, environment_id, target_id, source, reason, status)
			VALUES (?, ?, ?, ?, ?, ?, 'executed', 'manual', 'succeeded')`, deploymentID, orgA, app.ID(), releaseID, env.ID(), one.ID()).Error)
		require.NoError(t, f.db.Exec(`UPDATE delivery.application_targets SET current_deployment_id = ?, current_release_id = ? WHERE application_id = ? AND target_id = ?`,
			deploymentID, releaseID, app.ID(), one.ID()).Error)

		// configured was loaded before the Deployment set the current release.
		require.Nil(t, configured.CurrentDeploymentID())
		at(t, 3*time.Minute)
		require.NoError(t, configured.Configure(delivery.ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_values_c"}}))
		require.NoError(t, f.apps.UpdatePlacement(ctx, configured))

		got, err := f.apps.GetPlacement(ctx, orgA, app.ID(), one.ID())
		require.NoError(t, err)
		assert.Equal(t, new(deploymentID), got.CurrentDeploymentID())
		assert.Equal(t, new(releaseID), got.CurrentReleaseID())
		assert.Equal(t, configured.ApplyConfig(), got.ApplyConfig())
		assert.Equal(t, configured.UpdatedAt(), got.UpdatedAt())

		t.Run("delete refuses while deployments refer to it", func(t *testing.T) {
			assert.ErrorIs(t, f.apps.DeletePlacement(ctx, orgA, app.ID(), one.ID()), delivery.ErrInUse)
			_, err := f.apps.GetPlacement(ctx, orgA, app.ID(), one.ID())
			assert.NoError(t, err)
		})
	})

	t.Run("delete", func(t *testing.T) {
		require.NoError(t, f.apps.DeletePlacement(ctx, orgA, app.ID(), two.ID()))
		_, err := f.apps.GetPlacement(ctx, orgA, app.ID(), two.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		assert.ErrorIs(t, f.apps.DeletePlacement(ctx, orgA, app.ID(), two.ID()), delivery.ErrNotFound, "deleting again finds nothing")

		// A placement can be made again after it is removed.
		require.NoError(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, app.ID(), two.ID(), nil)))
	})
}

func TestApplicationRepositoryListRunsOn(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "runs-staging")
	target := f.createTarget(t, orgA, env.ID(), "runs-web")
	archivedTarget := f.createTarget(t, orgA, env.ID(), "runs-old")
	archivedTarget.Archive()
	require.NoError(t, f.targets.UpdateTarget(ctx, archivedTarget))
	app := f.createApp(t, orgA, wsA, projA, "runs-api")
	other := f.createApp(t, orgA, wsA, projA, "runs-worker")

	placement := newPlacement(orgA, app.ID(), target.ID(), nil)
	require.NoError(t, f.apps.CreatePlacement(ctx, placement))
	require.NoError(t, f.apps.CreatePlacement(ctx, newPlacement(orgA, app.ID(), archivedTarget.ID(), nil)))

	lfc := f.createLifecycle(t, orgA, wsA, "runs-mainline")
	ch := f.createChannel(t, orgA, app.ID(), lfc.ID(), "default", true)
	releaseID, deploymentID := "rel_"+id.ULID(), "dep_"+id.ULID()
	require.NoError(t, f.db.Exec(`INSERT INTO delivery.releases (id, organization_id, application_id, channel_id, version, status, source)
		VALUES (?, ?, ?, ?, '2.0.0', 'published', 'manual')`, releaseID, orgA, app.ID(), ch.ID()).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO delivery.deployments (id, organization_id, application_id, release_id, environment_id, target_id, source, reason, status)
		VALUES (?, ?, ?, ?, ?, ?, 'executed', 'rollout', 'healthy')`, deploymentID, orgA, app.ID(), releaseID, env.ID(), target.ID()).Error)
	require.NoError(t, f.db.Exec(`UPDATE delivery.application_targets SET current_release_id = ?, current_deployment_id = ? WHERE application_id = ? AND target_id = ?`,
		releaseID, deploymentID, app.ID(), target.ID()).Error)

	got, err := f.apps.ListRunsOn(ctx, orgA, []string{app.ID(), other.ID()})
	require.NoError(t, err)
	require.Len(t, got, 1, "only active Targets are returned")
	row := got[0]
	assert.Equal(t, app.ID(), row.ApplicationID)
	assert.Equal(t, target.ID(), row.TargetID)
	assert.Equal(t, "Target runs-web", row.TargetName)
	assert.Equal(t, env.ID(), row.EnvironmentID)
	assert.Equal(t, "Env runs-staging", row.EnvironmentName)
	assert.Equal(t, releaseID, *row.ReleaseID)
	assert.Equal(t, "2.0.0", *row.ReleaseVersion)
	assert.Equal(t, deploymentID, *row.DeploymentID)
	assert.Equal(t, release.DeploymentHealthy, *row.DeploymentStatus)

	got, err = f.apps.ListRunsOn(ctx, orgB, []string{app.ID()})
	require.NoError(t, err)
	assert.Empty(t, got, "another organization sees nothing")
}

func TestChannelRuleUpsertStaysInItsChannel(t *testing.T) {

	f := newFixture(t)
	ctx := f.ctx
	lfcB := f.createLifecycle(t, orgB, wsA, "mainline")
	appB := f.createApp(t, orgB, wsA, projA, "victim")
	victim := f.createChannel(t, orgB, appB.ID(), lfcB.ID(), "default", true, gitRefRule("refs/heads/main"))
	victimRule := victim.Rules()[0]

	lfcA := f.createLifecycle(t, orgA, wsA, "mainline")
	appA := f.createApp(t, orgA, wsA, projA, "api")
	mine := f.createChannel(t, orgA, appA.ID(), lfcA.ID(), "default", true)

	// A Channel object carrying a rule whose ID is already taken elsewhere.
	forged := must(delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: orgA, ApplicationID: appA.ID(), LifecycleID: lfcA.ID(),
		Slug: mine.Slug(), DisplayName: mine.DisplayName(), IsDefault: true,
		Rules: []delivery.NewChannelRuleProps{{
			ID: new(victimRule.ID), Kind: release.RuleGitRef, Config: json.RawMessage(`{"patterns":["refs/heads/attacker"]}`),
		}},
		CreatedAt: new(mine.CreatedAt()),
	}, new(mine.ID())))
	err := f.apps.UpdateChannel(ctx, forged)

	got := must(f.apps.GetChannel(ctx, orgB, victim.ID()))
	assert.Equal(t, victim.Snapshot(), got.Snapshot(), "another organization's rule is untouched")
	assert.Error(t, err, "the update cannot store a rule it does not own")
}
