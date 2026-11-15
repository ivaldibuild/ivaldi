package releases

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

var ctx = context.Background()

func TestNewReleaseServiceRequiresItsDependencies(t *testing.T) {
	w := newWorld(t)
	all := w.options(w.authz)
	for i := range all {
		without := append(append([]ReleaseServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewReleaseService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

func report(w *world, b delivery.BuildInput) (*delivery.Build, error) {
	return w.svc.ReportBuild(ctx, ada, delivery.ReportBuildInput{OrganizationID: org, ApplicationID: w.app.ID(), Build: b})
}

func TestReportBuild(t *testing.T) {
	w := newWorld(t)

	b, err := report(w, apiBuild("a"))
	require.NoError(t, err)
	s := b.Snapshot()
	assert.Equal(t, "ghcr.io/acme/api", s.ArtifactName, "the artifact's published name")
	assert.Equal(t, delivery.ArtifactContainerImage, s.ArtifactKind)
	assert.Equal(t, delivery.BuildCIReported, s.Provenance)
	assert.Equal(t, new("refs/tags/v1.4.0"), s.SourceRef)
	assert.Equal(t, []string{"delivery.v1.build.recorded"}, w.eventTypes())
	assert.Equal(t, []string{delivery.EventBuildRecorded}, w.auditTypes())
	assert.Equal(t, "dec-"+delivery.ActionBuildReport, w.audit[0].DecisionID)

	req := w.authz.asked[0]
	assert.Equal(t, delivery.PermissionReleaseManage, req.Permission)
	assert.Equal(t, project, req.ResourceID, "checked on the Application's Project")

	t.Run("the same report again changes nothing", func(t *testing.T) {
		again, err := report(w, apiBuild("a"))
		require.NoError(t, err)
		assert.Equal(t, b.ID(), again.ID())
		assert.Len(t, w.events, 1)
	})

	t.Run("an observed build is filled in", func(t *testing.T) {
		seen := must(delivery.NewBuild(delivery.NewBuildProps{
			OrganizationID: org, ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/api",
			Digest: digest("b"), Provenance: delivery.BuildObserved,
		}, nil))
		w.builds[seen.ID()] = seen.Snapshot()
		got, err := report(w, apiBuild("b"))
		require.NoError(t, err)
		assert.Equal(t, seen.ID(), got.ID())
		assert.Equal(t, delivery.BuildCIReported, got.Provenance())
		assert.Equal(t, delivery.EventBuildReported, w.audit[len(w.audit)-1].Type)
	})

	t.Run("another commit for a recorded digest is refused, and audited", func(t *testing.T) {
		forged := apiBuild("a")
		forged.Details.CommitSHA = new(strings.Repeat("f", 40))
		_, err := report(w, forged)
		assert.ErrorIs(t, err, delivery.ErrBuildConflict)
		last := w.audit[len(w.audit)-1]
		assert.Equal(t, delivery.EventBuildReportRefused, last.Type)
		assert.Equal(t, b.ID(), last.ResourceID)
		assert.Equal(t, map[string]string{"reported_commit_sha": strings.Repeat("f", 40)}, last.After)
		assert.Equal(t, strings.Repeat("a", 40), *w.builds[b.ID()].CommitSHA, "the build keeps its commit")
	})

	for name, tc := range map[string]struct {
		in   delivery.BuildInput
		want error
	}{
		"an unknown artifact":  {in: delivery.BuildInput{Artifact: "web", Digest: digest("c")}, want: delivery.ErrInvalid},
		"an archived artifact": {in: delivery.BuildInput{Artifact: "migrate", Digest: digest("c")}, want: delivery.ErrInvalid},
		"no digest":            {in: delivery.BuildInput{Artifact: "api"}, want: delivery.ErrInvalid},
		"a bad digest":         {in: delivery.BuildInput{Artifact: "api", Digest: "latest"}, want: delivery.ErrInvalid},
		"a digest and a tag":   {in: delivery.BuildInput{Artifact: "api", Digest: digest("c"), Tag: "v1"}, want: delivery.ErrInvalid},
		"a tag":                {in: delivery.BuildInput{Artifact: "api", Tag: "v1.4.0"}, want: delivery.ErrTagsUnsupported},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(w.builds)
			_, err := report(w, tc.in)
			assert.ErrorIs(t, err, tc.want)
			assert.Len(t, w.builds, before)
		})
	}

	t.Run("a tag the resolver knows becomes its digest", func(t *testing.T) {
		w.tags["ghcr.io/acme/chart:1.0.0"] = digest("d")
		got, err := report(w, delivery.BuildInput{Artifact: "chart", Tag: "1.0.0"})
		require.NoError(t, err)
		assert.Equal(t, digest("d"), got.Digest())
		assert.Equal(t, delivery.ArtifactHelmChart, got.ArtifactKind())
	})
}

func TestCreateReleaseJoinsTheChannelItMeets(t *testing.T) {
	w := newWorld(t)

	d := w.create(t, "1.4.0", apiBuild("a"))
	rel := d.Release
	assert.Equal(t, w.main.ID(), rel.ChannelID(), "no pre-release tag: main")
	assert.Equal(t, release.StatusDraft, rel.Status())
	assert.Equal(t, delivery.ReleaseCreated, rel.Source())
	require.Len(t, d.Builds, 1)
	assert.Equal(t, digest("a"), d.Builds[0].Digest(), "the build was recorded with it")
	assert.Equal(t, []string{"delivery.v1.build.recorded", "delivery.v1.release.created"}, w.eventTypes())
	assert.Equal(t, []string{delivery.EventBuildRecorded, delivery.EventReleaseCreated}, w.auditTypes())
	assert.Equal(t, "prn_ada", *rel.Snapshot().CreatedByPrincipalID)

	beta := w.create(t, "1.5.0-beta.1", apiBuild("b"))
	assert.Equal(t, w.beta.ID(), beta.Release.ChannelID())

	t.Run("a version is unique within the application", func(t *testing.T) {
		_, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.0", Builds: []delivery.BuildInput{apiBuild("c")}})
		assert.ErrorIs(t, err, delivery.ErrAlreadyExists)
		assert.Len(t, w.releases, 2)
		assert.Len(t, w.builds, 2, "the build recorded with it was rolled back too")
	})
}

// A Release deploys with the variables its Application had when it was
// created, so creating one snapshots them.
func TestCreateReleaseSnapshotsItsVariables(t *testing.T) {
	w := newWorld(t)
	d := w.create(t, "1.4.0", apiBuild("a"))
	assert.Equal(t, []string{d.Release.ID() + " by prn_ada in " + project}, w.snapshotted)

	_, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.0", Builds: []delivery.BuildInput{apiBuild("c")}})
	require.Error(t, err)
	assert.Len(t, w.snapshotted, 1, "a release that was not created has no variables")
}

func TestGetReleaseVariables(t *testing.T) {
	w := newWorld(t)
	rel := w.create(t, "1.4.0", apiBuild("a")).Release

	view, err := w.svc.GetReleaseVariables(ctx, ada, org, rel.ID(), "")
	require.NoError(t, err)
	assert.Nil(t, view.Revision, "a release with no snapshot deploys with no variables")
	assert.Empty(t, view.Resolved)

	environment := "env_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	target := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: environment, ClusterID: "cls_01BX5ZZKBKACTAV9WEVGEMMVRZ", Slug: "eu", DisplayName: "EU",
		Runtime: delivery.RuntimeKubernetes, Locator: map[string]string{delivery.LocatorCluster: "prod", delivery.LocatorNamespace: "payments"},
	}, nil))
	w.targets = map[string]*delivery.Target{target.ID(): target}
	w.current = map[string]*delivery.ReleaseVariables{rel.ID(): must(delivery.NewReleaseVariables(delivery.NewReleaseVariablesProps{
		OrganizationID: org, ApplicationID: w.app.ID(), ReleaseID: rel.ID(), Revision: 1,
		Variables: []delivery.SnapshotVariable{{
			Name: "Log.Level", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText,
			Values: []delivery.SnapshotValue{
				{Value: "info"},
				{Value: "warn", Scope: delivery.VariableScope{EnvironmentIDs: []string{environment}}},
			},
		}},
	}, nil))}

	view, err = w.svc.GetReleaseVariables(ctx, ada, org, rel.ID(), target.ID())
	require.NoError(t, err)
	require.NotNil(t, view.Revision)
	assert.Equal(t, 1, view.Revision.Revision)
	require.Len(t, view.Resolved, 1)
	assert.Equal(t, "warn", view.Resolved[0].Winner.Value)
	assert.Equal(t, "info", view.Resolved[0].Beaten[0].Value)

	_, err = w.svc.GetReleaseVariables(ctx, ada, org, rel.ID(), "tgt_01BX5ZZKBKACTAV9WEVGEMMVZZ")
	assert.ErrorIs(t, err, delivery.ErrInvalid, "an unknown target")
}

func TestCreateReleaseRecordsWhoTriggeredIt(t *testing.T) {
	w := newWorld(t)
	d, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{
		OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.1", Builds: []delivery.BuildInput{apiBuild("d")},
		TriggeredBy: "grace", TriggeredByEmail: "grace@example.com",
	})
	require.NoError(t, err)
	s := d.Release.Snapshot()
	assert.Equal(t, "grace", *s.TriggeredBy)
	assert.Equal(t, "grace@example.com", *s.TriggeredByEmail)
	assert.Equal(t, "prn_ada", *s.CreatedByPrincipalID, "the authenticated creator is recorded apart")
}

func TestCreateReleaseRefusals(t *testing.T) {
	cases := map[string]struct {
		in      delivery.CreateReleaseInput
		want    error
		message string
	}{
		"no builds": {in: delivery.CreateReleaseInput{Version: "1.0.0"}, want: delivery.ErrInvalid},
		"a triggered_by_email that is not an address": {
			in:   delivery.CreateReleaseInput{Version: "1.0.0", Builds: []delivery.BuildInput{apiBuild("a")}, TriggeredByEmail: "ada"},
			want: delivery.ErrInvalid,
		},
		"two builds of one artifact": {
			in:   delivery.CreateReleaseInput{Version: "1.0.0", Builds: []delivery.BuildInput{apiBuild("a"), apiBuild("b")}},
			want: delivery.ErrInvalid,
		},
		"a named channel whose rules it does not meet": {
			in:      delivery.CreateReleaseInput{Version: "2.0.0-rc.1", ChannelID: "main", Builds: []delivery.BuildInput{apiBuild("a")}},
			want:    delivery.ErrInvalid,
			message: `the release does not meet channel main's rules: the release version 2.0.0-rc.1 has pre-release tag "rc.1", which does not match ^$`,
		},
		"no channel admits it": {
			in:      delivery.CreateReleaseInput{Version: "2.0.0-rc.1", Builds: []delivery.BuildInput{apiBuild("a")}},
			want:    delivery.ErrInvalid,
			message: "no channel's rules admit the release",
		},
		"an archived channel": {
			in:   delivery.CreateReleaseInput{Version: "1.0.0", ChannelID: "old", Builds: []delivery.BuildInput{apiBuild("a")}},
			want: delivery.ErrArchived,
		},
		"an unknown channel": {
			in:   delivery.CreateReleaseInput{Version: "1.0.0", ChannelID: "chn_01ARZ3NDEKTSV4RRFFQ69G5FZZ", Builds: []delivery.BuildInput{apiBuild("a")}},
			want: delivery.ErrInvalid,
		},
		"a version with spaces": {
			in:   delivery.CreateReleaseInput{Version: "1.0 final", Builds: []delivery.BuildInput{apiBuild("a")}},
			want: delivery.ErrInvalid,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			in := tc.in
			in.OrganizationID, in.ApplicationID = org, w.app.ID()
			switch in.ChannelID {
			case "main":
				in.ChannelID = w.main.ID()
			case "old":
				in.ChannelID = w.old.ID()
			}
			_, err := w.svc.CreateRelease(ctx, ada, in)
			assert.ErrorIs(t, err, tc.want)
			if tc.message != "" {
				assert.Contains(t, err.Error(), tc.message)
			}
			assert.Empty(t, w.releases)
			assert.Empty(t, w.builds, "nothing half-written")
			assert.Empty(t, w.audit)
		})
	}

	t.Run("several channels admit it", func(t *testing.T) {
		w := newWorld(t)
		any := must(delivery.NewChannel(delivery.NewChannelProps{
			OrganizationID: org, ApplicationID: w.app.ID(), LifecycleID: w.main.LifecycleID(), Slug: "any", DisplayName: "Any",
		}, nil))
		w.channels = append(w.channels, any)
		_, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.0.0", Builds: []delivery.BuildInput{apiBuild("a")}})
		var rules *delivery.RulesError
		require.ErrorAs(t, err, &rules)
		assert.Equal(t, []string{"main", "any"}, rules.Candidates)
	})
}

func TestCreateAndPublishInOneCall(t *testing.T) {
	w := newWorld(t)
	d, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{
		OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.0", Builds: []delivery.BuildInput{apiBuild("a")}, Publish: true,
	})
	require.NoError(t, err)
	s := d.Release.Snapshot()
	assert.Equal(t, release.StatusPublished, s.Status)
	assert.Equal(t, new("dec-"+delivery.ActionReleasePublish), s.PublishDecisionID, "the publish decision, not the create one")
	assert.Equal(t, []string{delivery.PermissionReleaseManage, delivery.PermissionReleasePublish}, w.authz.permissions())
	assert.Equal(t, []string{delivery.EventBuildRecorded, delivery.EventReleaseCreated, delivery.EventReleasePublished}, w.auditTypes())
	assert.Equal(t, "dec-"+delivery.ActionReleaseCreate, w.audit[1].DecisionID)
	assert.Equal(t, "dec-"+delivery.ActionReleasePublish, w.audit[2].DecisionID)

	t.Run("someone who may create but not publish creates nothing", func(t *testing.T) {
		w := newWorld(t)
		w.authz.deny = func(r authorization.Request) bool { return r.Permission == delivery.PermissionReleasePublish }
		_, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{
			OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.0", Builds: []delivery.BuildInput{apiBuild("a")}, Publish: true,
		})
		assert.ErrorIs(t, err, authorization.ErrDenied)
		assert.Empty(t, w.releases)
	})

	t.Run("a required artifact without a build is not published", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.svc.CreateRelease(ctx, ada, delivery.CreateReleaseInput{
			OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.4.0",
			Builds: []delivery.BuildInput{{Artifact: "chart", Digest: digest("c")}}, Publish: true,
		})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Contains(t, err.Error(), "required artifact api")
		assert.Empty(t, w.releases, "not even as a draft")
	})
}

func TestPublishAndWithdraw(t *testing.T) {
	w := newWorld(t)
	d := w.create(t, "1.4.0", apiBuild("a"))
	id := d.Release.ID()

	published, err := w.svc.PublishRelease(ctx, ada, org, id)
	require.NoError(t, err)
	assert.Equal(t, release.StatusPublished, published.Release.Status())
	assert.Equal(t, new("dec-"+delivery.ActionReleasePublish), published.Release.PublishDecisionID())
	assert.Equal(t, digest("a"), published.Builds[0].Digest())
	req := w.authz.asked[len(w.authz.asked)-1]
	assert.Equal(t, delivery.PermissionReleasePublish, req.Permission)
	assert.Equal(t, project, req.ResourceID)

	_, err = w.svc.PublishRelease(ctx, ada, org, id)
	assert.ErrorIs(t, err, delivery.ErrReleaseStatus)

	withdrawn, err := w.svc.WithdrawRelease(ctx, ada, org, id)
	require.NoError(t, err)
	assert.Equal(t, release.StatusWithdrawn, withdrawn.Release.Status())
	assert.Equal(t, delivery.PermissionReleaseManage, w.authz.asked[len(w.authz.asked)-1].Permission)
	assert.Equal(t, []string{"delivery.v1.build.recorded", "delivery.v1.release.created", "delivery.v1.release.published", "delivery.v1.release.withdrawn"}, w.eventTypes())

	t.Run("a draft is not withdrawn", func(t *testing.T) {
		draft := w.create(t, "1.5.0", apiBuild("b"))
		_, err := w.svc.WithdrawRelease(ctx, ada, org, draft.Release.ID())
		assert.ErrorIs(t, err, delivery.ErrReleaseStatus)
	})

	t.Run("publishing checks the rules again", func(t *testing.T) {
		draft := w.create(t, "1.6.0", apiBuild("c"))
		_, err := w.main.AddRule(delivery.ChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"range":"[2.0,)"}`)})
		require.NoError(t, err)
		_, err = w.svc.PublishRelease(ctx, ada, org, draft.Release.ID())
		var rules *delivery.RulesError
		require.ErrorAs(t, err, &rules)
		assert.Contains(t, rules.Error(), "no longer meets channel main's rules")
	})

	t.Run("an unknown release", func(t *testing.T) {
		_, err := w.svc.PublishRelease(ctx, ada, org, "rel_01ARZ3NDEKTSV4RRFFQ69G5FZZ")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})
}

func TestReadReleases(t *testing.T) {
	w := newWorld(t)
	a := w.create(t, "1.4.0", apiBuild("a"))
	b := w.create(t, "1.5.0-beta.1", apiBuild("b"), delivery.BuildInput{Artifact: "chart", Digest: digest("c")})

	got, err := w.svc.GetRelease(ctx, ada, org, b.Release.ID())
	require.NoError(t, err)
	require.Len(t, got.Builds, 2, "with both builds")
	assert.Equal(t, delivery.PermissionApplicationRead, w.authz.asked[len(w.authz.asked)-1].Permission)

	page, err := w.svc.ListReleases(ctx, ada, delivery.ListReleasesQuery{OrganizationID: org, ApplicationID: w.app.ID(), ChannelID: w.main.ID()})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, a.Release.ID(), page.Items[0].Release.ID())
	assert.Equal(t, digest("a"), page.Items[0].Builds[0].Digest())

	_, err = w.svc.ListReleases(ctx, ada, delivery.ListReleasesQuery{OrganizationID: org, ApplicationID: w.app.ID(), Status: "deleted"})
	assert.ErrorIs(t, err, delivery.ErrInvalid)

	w.authz.deny = func(authorization.Request) bool { return true }
	_, err = w.svc.GetRelease(ctx, ada, org, a.Release.ID())
	assert.ErrorIs(t, err, authorization.ErrDenied)
}

func TestTheServiceCannotChangeAnythingWithoutADecision(t *testing.T) {
	w := newWorld(t)
	inner := w.svc.(*authorized).next
	_, err := inner.CreateRelease(ctx, ada, delivery.CreateReleaseInput{OrganizationID: org, ApplicationID: w.app.ID(), Version: "1.0.0", Builds: []delivery.BuildInput{apiBuild("a")}})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Empty(t, w.releases)
}

// Refreshing takes a new revision from the Application's variables as they
// are, unless a production Target the Application is placed on would get
// another value: that needs an approval that cannot be requested yet.
func TestRefreshReleaseVariables(t *testing.T) {
	const staging, production = "env_01BX5ZZKBKACTAV9WEVGEMMVR1", "env_01BX5ZZKBKACTAV9WEVGEMMVR2"
	level := func(everywhere, inProduction string) []delivery.SnapshotVariable {
		return []delivery.SnapshotVariable{{
			Name: "Log.Level", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText,
			Values: []delivery.SnapshotValue{
				{Value: everywhere},
				{Value: inProduction, Scope: delivery.VariableScope{EnvironmentIDs: []string{production}}},
			},
		}}
	}
	setup := func(t *testing.T) (*world, *delivery.Release, *delivery.Target) {
		w := newWorld(t)
		rel := w.create(t, "1.4.0", apiBuild("a")).Release
		env := func(id string, tier delivery.Tier) *delivery.Environment {
			return must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
				OrganizationID: org, WorkspaceID: ws, Slug: string(tier), DisplayName: string(tier), Tier: tier,
			}, &id))
		}
		target := func(environmentID, slug string) *delivery.Target {
			return must(delivery.NewTarget(delivery.NewTargetProps{
				OrganizationID: org, EnvironmentID: environmentID, ClusterID: "cls_01BX5ZZKBKACTAV9WEVGEMMVRZ", Slug: slug, DisplayName: slug,
				Runtime: delivery.RuntimeKubernetes, Locator: map[string]string{delivery.LocatorCluster: "c", delivery.LocatorNamespace: slug},
			}, nil))
		}
		prod, stage := target(production, "prod"), target(staging, "stage")
		w.targets = map[string]*delivery.Target{prod.ID(): prod, stage.ID(): stage}
		w.environments = []*delivery.Environment{env(staging, delivery.TierStaging), env(production, delivery.TierProduction)}
		w.lifecycle = must(delivery.NewLifecycle(delivery.NewLifecycleProps{
			OrganizationID: org, WorkspaceID: ws, DisplayName: "Standard",
			Phases: []delivery.NewLifecyclePhaseProps{{Name: "Staging", EnvironmentIDs: []string{staging}}, {Name: "Production", EnvironmentIDs: []string{production}}},
		}, nil))
		for _, tg := range []*delivery.Target{prod, stage} {
			w.placements = append(w.placements, must(delivery.NewPlacement(delivery.NewPlacementProps{
				OrganizationID: org, ApplicationID: w.app.ID(), TargetID: tg.ID(),
			})))
		}
		w.current = map[string]*delivery.ReleaseVariables{rel.ID(): must(delivery.NewReleaseVariables(delivery.NewReleaseVariablesProps{
			OrganizationID: org, ApplicationID: w.app.ID(), ReleaseID: rel.ID(), Revision: 1, Variables: level("info", "warn"),
		}, nil))}
		// A stored revision has no events left to record.
		w.current[rel.ID()].PullEvents()
		w.audit, w.events = nil, nil
		return w, rel, prod
	}

	t.Run("a change production does not get becomes the next revision", func(t *testing.T) {
		w, rel, _ := setup(t)
		w.preview = level("debug", "warn")

		preview, err := w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), true)
		require.NoError(t, err)
		assert.False(t, preview.Applied)
		assert.Equal(t, []string{"Log.Level"}, preview.Changed)
		assert.Empty(t, preview.Production, "production's value wins there, and it is the same")
		assert.Equal(t, 1, preview.Revision.Revision, "a preview takes no revision")

		refreshed, err := w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), false)
		require.NoError(t, err)
		assert.True(t, refreshed.Applied)
		assert.Equal(t, 2, refreshed.Revision.Revision)
		assert.Equal(t, []string{delivery.EventReleaseVariablesRefreshed}, w.auditTypes())
		assert.Equal(t, []string{"delivery.v1.release_variables.superseded", "delivery.v1.release_variables.taken"}, w.eventTypes())
	})

	t.Run("the revision holds what was checked, not an edit made since", func(t *testing.T) {
		w, rel, _ := setup(t)
		w.preview, w.edited = level("debug", "warn"), level("debug", "error")

		refreshed, err := w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), false)
		require.NoError(t, err)
		assert.True(t, refreshed.Applied)
		assert.Equal(t, level("debug", "warn"), w.stored, "production's value was not checked as error")
	})

	t.Run("a change to what production gets is refused", func(t *testing.T) {
		w, rel, prod := setup(t)
		w.preview = level("info", "error")

		preview, err := w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), true)
		require.NoError(t, err)
		assert.Equal(t, []delivery.ProductionChange{{TargetID: prod.ID(), EnvironmentID: production, Variables: []string{"Log.Level"}}}, preview.Production)

		_, err = w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), false)
		require.ErrorIs(t, err, delivery.ErrProductionChange)
		assert.Contains(t, err.Error(), "Log.Level")
		assert.Equal(t, 1, w.current[rel.ID()].Revision())
		assert.Empty(t, w.audit)
	})

	t.Run("nothing changed takes no revision", func(t *testing.T) {
		w, rel, _ := setup(t)
		w.preview = level("info", "warn")
		refreshed, err := w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), false)
		require.NoError(t, err)
		assert.False(t, refreshed.Applied)
		assert.Empty(t, refreshed.Changed)
		assert.Empty(t, w.audit)
	})

	t.Run("a withdrawn release keeps what it had", func(t *testing.T) {
		w, rel, _ := setup(t)
		w.preview = level("debug", "warn")
		_, err := w.svc.PublishRelease(ctx, ada, org, rel.ID())
		require.NoError(t, err)
		_, err = w.svc.WithdrawRelease(ctx, ada, org, rel.ID())
		require.NoError(t, err)
		_, err = w.svc.RefreshReleaseVariables(ctx, ada, org, rel.ID(), false)
		assert.ErrorIs(t, err, delivery.ErrReleaseStatus)
	})
}
