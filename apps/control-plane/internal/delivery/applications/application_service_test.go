package applications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestNewApplicationServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]ApplicationServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewApplicationService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewApplicationService(all...)
	assert.NoError(t, err)
}

// full is a world with one of everything, for tests that run every operation.
type full struct {
	*world
	env       *delivery.Environment
	target    *delivery.Target
	lifecycle *delivery.Lifecycle
	registry  *delivery.Registry
	app       *delivery.Application
	defaultCh *delivery.Channel
	artifact  *delivery.Artifact
	chart     *delivery.Artifact
	channel   *delivery.Channel // not the default, with a rule scoped to artifact
	archived  *delivery.Channel
}

func newFull(t *testing.T) *full {
	t.Helper()
	f := &full{world: newWorld(t)}
	f.env = f.world.env(t, wsActive, "staging")
	f.target = f.world.target(t, f.env, "web")
	f.lifecycle = f.world.lifecycle(t, wsActive, "mainline", false)
	f.registry = f.world.registry(t, wsActive, "ghcr", false)
	f.app, f.defaultCh = f.world.app(t, prjActive, "api", f.lifecycle)
	f.artifact = f.world.artifact(t, f.app, "api")
	f.chart = f.world.artifact(t, f.app, "chart")
	f.channel = f.world.channel(t, f.app, f.lifecycle, "hotfix", gitRule(ptr(f.artifact.ID())))
	f.archived = f.world.channel(t, f.app, f.lifecycle, "old")
	_, err := f.svc.ArchiveChannel(context.Background(), ada, org, f.archived.ID())
	require.NoError(t, err)
	f.world.placement(t, f.app, f.target, &delivery.ApplyConfig{DefinitionArtifact: "chart"})
	f.reset()
	return f
}

// authzCase is one operation and the request it must send.
type authzCase struct {
	name       string
	permission string
	action     string
	resource   string // prjActive unless set
	proposal   func(f *full) any
	call       func(f *full) error
}

func authzCases() []authzCase {
	ctx := context.Background()
	none := func(*full) any { return nil }
	manage, read := delivery.PermissionApplicationManage, delivery.PermissionApplicationRead
	createApp := func(f *full) delivery.CreateApplicationInput {
		return delivery.CreateApplicationInput{OrganizationID: org, ProjectID: prjActive, Slug: "worker", DisplayName: "Worker", DefaultLifecycleID: f.lifecycle.ID()}
	}
	createArtifact := func(f *full) delivery.CreateArtifactInput {
		return delivery.CreateArtifactInput{OrganizationID: org, ApplicationID: f.app.ID(), Name: "migrate", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"}
	}
	createChannel := func(f *full) delivery.CreateChannelInput {
		return delivery.CreateChannelInput{OrganizationID: org, ApplicationID: f.app.ID(), LifecycleID: f.lifecycle.ID(), Slug: "beta", DisplayName: "Beta"}
	}

	return []authzCase{
		{"CreateApplication", manage, delivery.ActionApplicationCreate, "", func(f *full) any { return createApp(f) }, func(f *full) error {
			_, _, err := f.svc.CreateApplication(ctx, ada, createApp(f))
			return err
		}},
		{"GetApplication", read, delivery.ActionApplicationRead, "", none, func(f *full) error {
			_, err := f.svc.GetApplication(ctx, ada, org, f.app.ID())
			return err
		}},
		{"ListApplications in a project", read, delivery.ActionApplicationList, "", none, func(f *full) error {
			_, err := f.svc.ListApplications(ctx, ada, delivery.ListApplicationsQuery{OrganizationID: org, WorkspaceID: wsActive, ProjectID: prjActive})
			return err
		}},
		{"ListApplications in a workspace", read, delivery.ActionApplicationList, wsActive, none, func(f *full) error {
			_, err := f.svc.ListApplications(ctx, ada, delivery.ListApplicationsQuery{OrganizationID: org, WorkspaceID: wsActive})
			return err
		}},
		{"UpdateApplication", manage, delivery.ActionApplicationUpdate, "", func(*full) any { return delivery.UpdateApplicationProps{DisplayName: ptr("API")} }, func(f *full) error {
			_, err := f.svc.UpdateApplication(ctx, ada, org, f.app.ID(), delivery.UpdateApplicationProps{DisplayName: ptr("API")})
			return err
		}},
		{"LinkApplicationSource", manage, delivery.ActionApplicationLinkSource, "", func(*full) any { return "src_1" }, func(f *full) error {
			_, err := f.svc.LinkApplicationSource(ctx, ada, org, f.app.ID(), "src_1")
			return err
		}},
		{"TransferApplication", manage, delivery.ActionApplicationTransfer, "", func(*full) any { return teamOther }, func(f *full) error {
			_, err := f.svc.TransferApplication(ctx, ada, org, f.app.ID(), teamOther)
			return err
		}},
		{"ArchiveApplication", manage, delivery.ActionApplicationArchive, "", none, func(f *full) error {
			_, err := f.svc.ArchiveApplication(ctx, ada, org, f.app.ID())
			return err
		}},
		{"RestoreApplication", manage, delivery.ActionApplicationRestore, "", none, func(f *full) error {
			_, err := f.svc.RestoreApplication(ctx, ada, org, f.app.ID())
			return err
		}},
		{"CreateArtifact", manage, delivery.ActionArtifactCreate, "", func(f *full) any { return createArtifact(f) }, func(f *full) error {
			_, err := f.svc.CreateArtifact(ctx, ada, createArtifact(f))
			return err
		}},
		{"ListArtifacts", read, delivery.ActionArtifactList, "", none, func(f *full) error {
			_, err := f.svc.ListArtifacts(ctx, ada, org, f.app.ID(), false)
			return err
		}},
		{"UpdateArtifact", manage, delivery.ActionArtifactUpdate, "", func(*full) any { return delivery.UpdateArtifactProps{Required: ptr(true)} }, func(f *full) error {
			_, err := f.svc.UpdateArtifact(ctx, ada, org, f.artifact.ID(), delivery.UpdateArtifactProps{Required: ptr(true)})
			return err
		}},
		{"SetArtifactRegistry", manage, delivery.ActionArtifactSetRegistry, "", func(f *full) any { return f.registry.ID() }, func(f *full) error {
			_, err := f.svc.SetArtifactRegistry(ctx, ada, org, f.artifact.ID(), f.registry.ID())
			return err
		}},
		{"DeclareArtifact", manage, delivery.ActionArtifactDeclare, "", none, func(f *full) error {
			_, err := f.svc.DeclareArtifact(ctx, ada, org, f.artifact.ID())
			return err
		}},
		{"ArchiveArtifact", manage, delivery.ActionArtifactArchive, "", none, func(f *full) error {
			_, err := f.svc.ArchiveArtifact(ctx, ada, org, f.artifact.ID())
			return err
		}},
		{"RestoreArtifact", manage, delivery.ActionArtifactRestore, "", none, func(f *full) error {
			_, err := f.svc.RestoreArtifact(ctx, ada, org, f.artifact.ID())
			return err
		}},
		{"CreateChannel", manage, delivery.ActionChannelCreate, "", func(f *full) any { return createChannel(f) }, func(f *full) error {
			_, err := f.svc.CreateChannel(ctx, ada, createChannel(f))
			return err
		}},
		{"GetChannel", read, delivery.ActionChannelRead, "", none, func(f *full) error {
			_, err := f.svc.GetChannel(ctx, ada, org, f.channel.ID())
			return err
		}},
		{"ListChannels", read, delivery.ActionChannelList, "", none, func(f *full) error {
			_, err := f.svc.ListChannels(ctx, ada, org, f.app.ID(), false)
			return err
		}},
		{"UpdateChannel", manage, delivery.ActionChannelUpdate, "", func(*full) any { return delivery.UpdateChannelProps{DisplayName: ptr("Hotfixes")} }, func(f *full) error {
			_, err := f.svc.UpdateChannel(ctx, ada, org, f.channel.ID(), delivery.UpdateChannelProps{DisplayName: ptr("Hotfixes")})
			return err
		}},
		{"SetChannelLifecycle", manage, delivery.ActionChannelSetLifecycle, "", func(f *full) any { return f.lifecycle.ID() }, func(f *full) error {
			_, err := f.svc.SetChannelLifecycle(ctx, ada, org, f.channel.ID(), f.lifecycle.ID())
			return err
		}},
		{"AddChannelRule", manage, delivery.ActionChannelAddRule, "", func(*full) any { return gitRule(nil) }, func(f *full) error {
			_, err := f.svc.AddChannelRule(ctx, ada, org, f.channel.ID(), gitRule(nil))
			return err
		}},
		{"UpdateChannelRule", manage, delivery.ActionChannelUpdateRule, "", func(*full) any { return gitRule(nil) }, func(f *full) error {
			_, err := f.svc.UpdateChannelRule(ctx, ada, org, f.channel.ID(), f.channel.Rules()[0].ID, gitRule(nil))
			return err
		}},
		{"RemoveChannelRule", manage, delivery.ActionChannelRemoveRule, "", func(f *full) any { return f.channel.Rules()[0].ID }, func(f *full) error {
			_, err := f.svc.RemoveChannelRule(ctx, ada, org, f.channel.ID(), f.channel.Rules()[0].ID)
			return err
		}},
		{"SetDefaultChannel", manage, delivery.ActionChannelSetDefault, "", func(f *full) any { return f.channel.ID() }, func(f *full) error {
			_, err := f.svc.SetDefaultChannel(ctx, ada, org, f.channel.ID())
			return err
		}},
		{"ArchiveChannel", manage, delivery.ActionChannelArchive, "", none, func(f *full) error {
			_, err := f.svc.ArchiveChannel(ctx, ada, org, f.channel.ID())
			return err
		}},
		{"RestoreChannel", manage, delivery.ActionChannelRestore, "", none, func(f *full) error {
			_, err := f.svc.RestoreChannel(ctx, ada, org, f.archived.ID())
			return err
		}},
		{"CreatePlacement", manage, delivery.ActionPlacementCreate, "", func(f *full) any {
			return delivery.CreatePlacementInput{OrganizationID: org, ApplicationID: f.app.ID(), TargetID: f.target.ID()}
		}, func(f *full) error {
			_, err := f.svc.CreatePlacement(ctx, ada, delivery.CreatePlacementInput{OrganizationID: org, ApplicationID: f.app.ID(), TargetID: f.target.ID()})
			return err
		}},
		{"ListPlacements", read, delivery.ActionPlacementList, "", none, func(f *full) error {
			_, err := f.svc.ListPlacements(ctx, ada, org, f.app.ID())
			return err
		}},
		{"ConfigurePlacement", manage, delivery.ActionPlacementConfigure, "", func(*full) any { return (*delivery.ApplyConfig)(nil) }, func(f *full) error {
			_, err := f.svc.ConfigurePlacement(ctx, ada, org, f.app.ID(), f.target.ID(), nil)
			return err
		}},
		{"RemovePlacement", manage, delivery.ActionPlacementRemove, "", func(f *full) any { return f.target.ID() }, func(f *full) error {
			return f.svc.RemovePlacement(ctx, ada, org, f.app.ID(), f.target.ID())
		}},
	}
}

// Every operation asks the authorizer one question, about the right
// resource, before it does anything.
func TestOperationsAreAuthorizedOnTheProject(t *testing.T) {
	for _, tc := range authzCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newFull(t)

			_ = tc.call(f) // Some calls fail on rules checked after authorizing; only the request matters here.

			require.Len(t, f.authz.asked, 1, "one request per operation")
			req := f.authz.asked[0]
			want := tc.resource
			if want == "" {
				want = prjActive
			}
			assert.Equal(t, tc.permission, req.Permission)
			assert.Equal(t, tc.action, req.Action)
			assert.Equal(t, want, req.ResourceID)
			assert.Equal(t, org, req.OrganizationID)
			assert.Equal(t, ada, req.Actor)
			assert.Equal(t, tc.proposal(f), req.Proposal)
		})
	}
}

// A refusal, whether a denial or a request for approval, writes nothing.
func TestRefusalsWriteNothing(t *testing.T) {
	outcomes := map[authorization.Outcome]error{
		authorization.OutcomeDeny:             authorization.ErrDenied,
		authorization.OutcomeRequiresApproval: authorization.ErrApprovalRequired,
		authorization.Outcome("unknown"):      authorization.ErrDenied,
	}
	for outcome, wantErr := range outcomes {
		for _, tc := range authzCases() {
			t.Run(string(outcome)+"/"+tc.name, func(t *testing.T) {
				f := newFull(t)
				f.authz.outcome = outcome
				before := f.state.clone()

				err := tc.call(f)

				require.ErrorIs(t, err, wantErr)
				assert.Equal(t, before, f.state)
			})
		}
	}
}

func TestApprovalRequiredCarriesTheDecision(t *testing.T) {
	f := newFull(t)
	f.authz.outcome = authorization.OutcomeRequiresApproval

	_, err := f.svc.UpdateApplication(context.Background(), ada, org, f.app.ID(), delivery.UpdateApplicationProps{DisplayName: ptr("API")})

	var approval *authorization.ApprovalRequiredError
	require.ErrorAs(t, err, &approval)
	assert.Equal(t, "dec-"+prjActive, approval.DecisionID, "the caller resubmits with this decision once approved")
}

// The service under the wrapper refuses to change anything without a
// decision, so wiring it up without authorization fails loudly.
func TestServiceWithoutAuthorizationChangesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFull(t)
	core := f.svc.(*authorized).next
	before := f.state.clone()

	_, _, err := core.CreateApplication(ctx, ada, delivery.CreateApplicationInput{OrganizationID: org, ProjectID: prjActive, Slug: "worker", DisplayName: "Worker", DefaultLifecycleID: f.lifecycle.ID()})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateApplication(ctx, ada, org, f.app.ID(), delivery.UpdateApplicationProps{DisplayName: ptr("API 2")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveApplication(ctx, ada, org, f.app.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.CreateArtifact(ctx, ada, delivery.CreateArtifactInput{OrganizationID: org, ApplicationID: f.app.ID(), Name: "migrate", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateArtifact(ctx, ada, org, f.artifact.ID(), delivery.UpdateArtifactProps{Required: ptr(true)})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.CreateChannel(ctx, ada, delivery.CreateChannelInput{OrganizationID: org, ApplicationID: f.app.ID(), LifecycleID: f.lifecycle.ID(), Slug: "beta", DisplayName: "Beta"})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.SetDefaultChannel(ctx, ada, org, f.channel.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveChannel(ctx, ada, org, f.channel.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ConfigurePlacement(ctx, ada, org, f.app.ID(), f.target.ID(), nil)
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	err = core.RemovePlacement(ctx, ada, org, f.app.ID(), f.target.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)

	assert.Equal(t, before, f.state)
}

func TestCreateApplication(t *testing.T) {
	ctx := context.Background()

	t.Run("created with its default channel", func(t *testing.T) {
		w := newWorld(t)
		l := w.lifecycle(t, wsActive, "mainline", false)

		app, ch, err := w.svc.CreateApplication(ctx, ada, delivery.CreateApplicationInput{
			OrganizationID: org, ProjectID: prjActive, DisplayName: "Checkout API", SourceID: "src_1", DefaultLifecycleID: l.ID(),
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(delivery.Application{}.Prefix(), app.ID()))
		assert.Equal(t, "checkout-api", app.Slug())
		assert.Equal(t, wsActive, app.WorkspaceID(), "the workspace is copied from the project")
		assert.Equal(t, prjActive, app.ProjectID())
		assert.Equal(t, teamOwner, app.OwnerTeamID(), "the owner defaults to the project's owning team")
		assert.Equal(t, "src_1", *app.SourceID())
		assert.Equal(t, map[string]string{delivery.WorkloadSelectorLabel: app.ID()}, app.WorkloadSelector())

		assert.True(t, id.Valid(delivery.Channel{}.Prefix(), ch.ID()))
		assert.True(t, ch.IsDefault())
		assert.Equal(t, delivery.DefaultChannelSlug, ch.Slug())
		assert.Equal(t, delivery.DefaultChannelName, ch.DisplayName())
		assert.Equal(t, app.ID(), ch.ApplicationID())
		assert.Equal(t, l.ID(), ch.LifecycleID())

		require.Len(t, w.audit, 2)
		assert.Equal(t, delivery.EventApplicationCreated, w.audit[0].Type)
		assert.Equal(t, app.ID(), w.audit[0].ResourceID)
		assert.Equal(t, app.Snapshot(), w.audit[0].After)
		assert.Equal(t, delivery.EventChannelCreated, w.audit[1].Type)
		assert.Equal(t, ch.ID(), w.audit[1].ResourceID)
		assert.Equal(t, ch.Snapshot(), w.audit[1].After)
		for _, e := range w.audit {
			assert.Equal(t, "dec-"+prjActive, e.DecisionID, "both records carry the one decision")
			assert.Equal(t, ada, e.Actor)
		}

		assert.Equal(t, app.Snapshot(), w.applications[app.ID()])
		assert.Equal(t, ch.Snapshot(), w.channels[ch.ID()])

		require.Len(t, w.events, 2, "one transaction can change two objects")
		assert.Equal(t, "delivery.v1.application.created", w.events[0].Type)
		assert.Equal(t, delivery.AggregateApplication, w.events[0].AggregateType)
		assert.Equal(t, app.ID(), w.events[0].AggregateID)
		assert.Equal(t, "delivery.v1.channel.created", w.events[1].Type)
		assert.Equal(t, delivery.AggregateChannel, w.events[1].AggregateType)
		assert.Equal(t, ch.ID(), w.events[1].AggregateID)
	})

	t.Run("with another active owner", func(t *testing.T) {
		w := newWorld(t)
		l := w.lifecycle(t, wsActive, "mainline", false)
		app, _, err := w.svc.CreateApplication(ctx, ada, delivery.CreateApplicationInput{OrganizationID: org, ProjectID: prjActive, OwnerTeamID: teamOther, DisplayName: "API", DefaultLifecycleID: l.ID()})
		require.NoError(t, err)
		assert.Equal(t, teamOther, app.OwnerTeamID())
	})

	t.Run("bad input is refused before asking", func(t *testing.T) {
		for name, in := range map[string]delivery.CreateApplicationInput{
			"no lifecycle":     {OrganizationID: org, ProjectID: prjActive, DisplayName: "API"},
			"no display name":  {OrganizationID: org, ProjectID: prjActive, DefaultLifecycleID: id.New(delivery.Lifecycle{}.Prefix())},
			"a bad slug":       {OrganizationID: org, ProjectID: prjActive, Slug: "Not A Slug", DisplayName: "API", DefaultLifecycleID: id.New(delivery.Lifecycle{}.Prefix())},
			"a bad selector":   {OrganizationID: org, ProjectID: prjActive, DisplayName: "API", DefaultLifecycleID: id.New(delivery.Lifecycle{}.Prefix()), WorkloadSelector: map[string]string{"": "x"}},
			"a lifecycle typo": {OrganizationID: org, ProjectID: prjActive, DisplayName: "API", DefaultLifecycleID: "env_01JZ0000000000000000000001"},
		} {
			t.Run(name, func(t *testing.T) {
				w := newWorld(t)
				_, _, err := w.svc.CreateApplication(ctx, ada, in)
				require.ErrorIs(t, err, delivery.ErrInvalid)
				assert.Empty(t, w.authz.asked)
			})
		}
	})

	refusals := []struct {
		name    string
		setup   func(*testing.T, *world) delivery.CreateApplicationInput
		wantErr error
	}{
		{"inactive owner", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{OwnerTeamID: teamGone, ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, delivery.ErrInactiveTeam},
		{"unknown owner", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{OwnerTeamID: "tem_nobody", ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, delivery.ErrInactiveTeam},
		{"the project's owner is inactive", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			w.teams[teamOwner] = false
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, delivery.ErrInactiveTeam},
		{"archived project", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			w.archiveProject(prjActive)
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, delivery.ErrArchived},
		{"missing project", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{ProjectID: "prj_01JZ0000000000000000000099", DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, delivery.ErrNotFound},
		{"lifecycle in another workspace", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsOther, "l", false).ID()}
		}, delivery.ErrWorkspaceMismatch},
		{"archived lifecycle", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", true).ID()}
		}, delivery.ErrArchived},
		{"missing lifecycle", func(*testing.T, *world) delivery.CreateApplicationInput {
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: "lfc_01JZ0000000000000000000099"}
		}, delivery.ErrNotFound},
		{"slug taken", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			l := w.lifecycle(t, wsActive, "l", false)
			w.app(t, prjActive, "api", l)
			return delivery.CreateApplicationInput{ProjectID: prjActive, Slug: "api", DefaultLifecycleID: l.ID()}
		}, delivery.ErrSlugTaken},
		{"a failing default channel rolls back the application", func(t *testing.T, w *world) delivery.CreateApplicationInput {
			w.fail["CreateChannel"] = errBoom
			return delivery.CreateApplicationInput{ProjectID: prjActive, DefaultLifecycleID: w.lifecycle(t, wsActive, "l", false).ID()}
		}, errBoom},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			in := tt.setup(t, w)
			in.OrganizationID = org
			in.DisplayName = "API"
			before := w.state.clone()

			_, _, err := w.svc.CreateApplication(ctx, ada, in)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, w.state)
		})
	}
}

func TestGetAndListApplications(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "l", false)
	api, _ := w.app(t, prjActive, "api", l)
	worker, _ := w.app(t, prjActive, "worker", l)
	_, err := w.svc.TransferApplication(ctx, ada, org, worker.ID(), teamOther)
	require.NoError(t, err)
	w.app(t, prjOther, "etl", w.lifecycle(t, wsOther, "l", false))

	got, err := w.svc.GetApplication(ctx, ada, org, api.ID())
	require.NoError(t, err)
	assert.Equal(t, api.Snapshot(), got.Snapshot())

	_, err = w.svc.GetApplication(ctx, ada, org, missingApp)
	require.ErrorIs(t, err, delivery.ErrNotFound)

	page, err := w.svc.ListApplications(ctx, ada, delivery.ListApplicationsQuery{OrganizationID: org, ProjectID: prjActive})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)

	page, err = w.svc.ListApplications(ctx, ada, delivery.ListApplicationsQuery{OrganizationID: org, WorkspaceID: wsActive, OwnerTeamID: teamOther})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, worker.ID(), page.Items[0].ID())

	w.reset()
	_, err = w.svc.ListApplications(ctx, ada, delivery.ListApplicationsQuery{OrganizationID: org})
	require.ErrorIs(t, err, delivery.ErrInvalid, "a list needs a project or a workspace")
	assert.Empty(t, w.authz.asked)

	w.authz.deny = func(authorization.Request) bool { return true }
	got, err = w.svc.GetApplication(ctx, ada, org, api.ID())
	require.ErrorIs(t, err, authorization.ErrDenied)
	assert.Nil(t, got)
}

func TestChangeApplication(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "l", false)
	app, _ := w.app(t, prjActive, "api", l)
	w.app(t, prjActive, "worker", l)

	t.Run("update", func(t *testing.T) {
		got, err := w.svc.UpdateApplication(ctx, ada, org, app.ID(), delivery.UpdateApplicationProps{WorkloadSelector: map[string]string{"app": "api"}})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "api"}, got.WorkloadSelector())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventApplicationUpdated, ev.Type)
		assert.Equal(t, "dec-"+prjActive, ev.DecisionID)

		_, err = w.svc.UpdateApplication(ctx, ada, org, app.ID(), delivery.UpdateApplicationProps{Slug: ptr("worker")})
		require.ErrorIs(t, err, delivery.ErrSlugTaken)
	})

	t.Run("link and unlink the source", func(t *testing.T) {
		got, err := w.svc.LinkApplicationSource(ctx, ada, org, app.ID(), "src_9")
		require.NoError(t, err)
		assert.Equal(t, "src_9", *got.SourceID())

		got, err = w.svc.LinkApplicationSource(ctx, ada, org, app.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.SourceID())
		assert.Equal(t, delivery.EventApplicationUpdated, w.lastAudit().Type)
	})

	t.Run("transfer", func(t *testing.T) {
		got, err := w.svc.TransferApplication(ctx, ada, org, app.ID(), teamOther)
		require.NoError(t, err)
		assert.Equal(t, teamOther, got.OwnerTeamID())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventApplicationOwnerChanged, ev.Type)
		assert.Equal(t, teamOwner, ev.Before.(delivery.ApplicationSnapshot).OwnerTeamID)
		assert.Equal(t, teamOther, ev.After.(delivery.ApplicationSnapshot).OwnerTeamID)
	})

	t.Run("transfer to an inactive team", func(t *testing.T) {
		before := w.state.clone()
		_, err := w.svc.TransferApplication(ctx, ada, org, app.ID(), teamGone)
		require.ErrorIs(t, err, delivery.ErrInactiveTeam)
		_, err = w.svc.TransferApplication(ctx, ada, org, app.ID(), "")
		require.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Equal(t, before, w.state)
	})

	t.Run("archive", func(t *testing.T) {
		got, err := w.svc.ArchiveApplication(ctx, ada, org, app.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, delivery.EventApplicationArchived, w.lastAudit().Type)

		n := len(w.audit)
		_, err = w.svc.ArchiveApplication(ctx, ada, org, app.ID())
		require.NoError(t, err)
		assert.Len(t, w.audit, n, "archiving an archived application records nothing")

		_, err = w.svc.UpdateApplication(ctx, ada, org, app.ID(), delivery.UpdateApplicationProps{DisplayName: ptr("X")})
		require.ErrorIs(t, err, delivery.ErrArchived)
		_, err = w.svc.TransferApplication(ctx, ada, org, app.ID(), teamOwner)
		require.ErrorIs(t, err, delivery.ErrArchived)
	})

	t.Run("restore is refused in an archived project", func(t *testing.T) {
		w.archiveProject(prjActive)
		before := w.state.clone()
		_, err := w.svc.RestoreApplication(ctx, ada, org, app.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, w.state)
	})

	t.Run("restore", func(t *testing.T) {
		w.projects[prjActive].Restore()
		got, err := w.svc.RestoreApplication(ctx, ada, org, app.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, delivery.EventApplicationRestored, w.lastAudit().Type)
	})
}

// An archived Application's Channels do not hold their Lifecycle, so
// restoring it waits for the Lifecycle to be restored.
func TestRestoreApplicationNeedsItsLifecycles(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	solo := w.lifecycle(t, wsActive, "solo", false)
	app, _ := w.app(t, prjActive, "batch", solo)
	w.archiveApp(t, app)

	n, err := w.CountActiveChannelsWithLifecycle(ctx, org, solo.ID())
	require.NoError(t, err)
	assert.Zero(t, n, "only active applications' channels hold a lifecycle")
	solo.Archive()
	w.setLifecycle(solo)

	before := w.state.clone()
	_, err = w.svc.RestoreApplication(ctx, ada, org, app.ID())
	assert.ErrorIs(t, err, delivery.ErrArchived, "its default channel's lifecycle is archived")
	assert.Equal(t, before, w.state)

	solo.Restore()
	w.setLifecycle(solo)
	restored, err := w.svc.RestoreApplication(ctx, ada, org, app.ID())
	require.NoError(t, err)
	assert.False(t, restored.Archived())
}
