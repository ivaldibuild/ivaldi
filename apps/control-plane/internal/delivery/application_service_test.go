package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestServiceCreateApplication(t *testing.T) {
	t.Run("created with its default channel", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "mainline")
		before := f.audit.count()

		app, ch, err := f.svc.CreateApplication(svcCtx, svcAlice, CreateApplicationInput{
			OrganizationID: svcOrg, ProjectID: f.project.ID(), DisplayName: "Checkout API", SourceID: "src_1", DefaultLifecycleID: l.ID(),
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Application{}.Prefix(), app.ID()))
		assert.Equal(t, "checkout-api", app.Slug())
		assert.Equal(t, f.ws.ID(), app.WorkspaceID(), "the workspace is copied from the project")
		assert.Equal(t, f.project.ID(), app.ProjectID())
		assert.Equal(t, svcTeam, app.OwnerTeamID(), "the owner defaults to the project's owning team")
		assert.Equal(t, "src_1", *app.SourceID())
		assert.Equal(t, map[string]string{WorkloadSelectorLabel: app.ID()}, app.WorkloadSelector())

		assert.True(t, id.Valid(Channel{}.Prefix(), ch.ID()))
		assert.True(t, ch.IsDefault())
		assert.Equal(t, DefaultChannelSlug, ch.Slug())
		assert.Equal(t, DefaultChannelName, ch.DisplayName())
		assert.Equal(t, app.ID(), ch.ApplicationID())
		assert.Equal(t, l.ID(), ch.LifecycleID())

		events := f.audit.since(before)
		require.Len(t, events, 2)
		assert.Equal(t, EventApplicationCreated, events[0].Type)
		assert.Equal(t, app.ID(), events[0].ResourceID)
		assert.Equal(t, app.Snapshot(), events[0].After)
		assert.Equal(t, EventChannelCreated, events[1].Type)
		assert.Equal(t, ch.ID(), events[1].ResourceID)
		assert.Equal(t, ch.Snapshot(), events[1].After)

		storedApp, err := f.store.GetApplication(svcCtx, svcOrg, app.ID())
		require.NoError(t, err)
		assert.Equal(t, app.Snapshot(), storedApp.Snapshot())
		storedCh, err := f.store.GetDefaultChannel(svcCtx, svcOrg, app.ID())
		require.NoError(t, err)
		assert.Equal(t, ch.Snapshot(), storedCh.Snapshot())
	})

	t.Run("with another active owner", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "mainline")
		app, _, err := f.svc.CreateApplication(svcCtx, svcAlice, CreateApplicationInput{OrganizationID: svcOrg, ProjectID: f.project.ID(), OwnerTeamID: svcOtherTeam, DisplayName: "API", DefaultLifecycleID: l.ID()})
		require.NoError(t, err)
		assert.Equal(t, svcOtherTeam, app.OwnerTeamID())
	})

	refusals := []struct {
		name    string
		setup   func(*svcFixture) CreateApplicationInput
		wantErr error
	}{
		{"inactive owner", func(f *svcFixture) CreateApplicationInput {
			return CreateApplicationInput{OwnerTeamID: svcGoneTeam, ProjectID: f.project.ID(), DefaultLifecycleID: f.lifecycle(t, "l").ID()}
		}, ErrInactiveTeam},
		{"unknown owner", func(f *svcFixture) CreateApplicationInput {
			return CreateApplicationInput{OwnerTeamID: "tem_nobody", ProjectID: f.project.ID(), DefaultLifecycleID: f.lifecycle(t, "l").ID()}
		}, ErrInactiveTeam},
		{"the project's owner is inactive", func(f *svcFixture) CreateApplicationInput {
			f.teams[svcOrg+"/"+svcTeam] = false
			return CreateApplicationInput{ProjectID: f.project.ID(), DefaultLifecycleID: f.lifecycle(t, "l").ID()}
		}, ErrInactiveTeam},
		{"archived project", func(f *svcFixture) CreateApplicationInput {
			l := f.lifecycle(t, "l")
			f.archiveProject(f.project)
			return CreateApplicationInput{ProjectID: f.project.ID(), DefaultLifecycleID: l.ID()}
		}, ErrArchived},
		{"missing project", func(f *svcFixture) CreateApplicationInput {
			return CreateApplicationInput{ProjectID: "prj_01ARZ3NDEKTSV4RRFFQ69G5FAW", DefaultLifecycleID: f.lifecycle(t, "l").ID()}
		}, ErrNotFound},
		{"lifecycle in another workspace", func(f *svcFixture) CreateApplicationInput {
			other, _ := f.addWorkspace(t, "data", "etl")
			return CreateApplicationInput{ProjectID: f.project.ID(), DefaultLifecycleID: f.lifecycleIn(t, other.ID(), "l").ID()}
		}, ErrWorkspaceMismatch},
		{"archived lifecycle", func(f *svcFixture) CreateApplicationInput {
			l := f.lifecycle(t, "l")
			_, err := f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
			require.NoError(t, err)
			return CreateApplicationInput{ProjectID: f.project.ID(), DefaultLifecycleID: l.ID()}
		}, ErrArchived},
		{"missing lifecycle", func(f *svcFixture) CreateApplicationInput {
			return CreateApplicationInput{ProjectID: f.project.ID(), DefaultLifecycleID: "lfc_01ARZ3NDEKTSV4RRFFQ69G5FAW"}
		}, ErrNotFound},
		{"slug taken", func(f *svcFixture) CreateApplicationInput {
			l := f.lifecycle(t, "l")
			f.app(t, "api", l)
			return CreateApplicationInput{ProjectID: f.project.ID(), Slug: "api", DefaultLifecycleID: l.ID()}
		}, ErrSlugTaken},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			f := newSvcFixture(t)
			in := tt.setup(f)
			in.OrganizationID = svcOrg
			in.DisplayName = "API"
			state, events, resources := f.marks()

			_, _, err := f.svc.CreateApplication(svcCtx, svcAlice, in)

			require.ErrorIs(t, err, tt.wantErr)
			f.requireUnchanged(t, state, events, resources)
		})
	}

	t.Run("a failing default channel rolls back the application", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "l")
		state, events, resources := f.marks()
		f.store.fail["CreateChannel"] = svcErrBoom

		_, _, err := f.svc.CreateApplication(svcCtx, svcAlice, CreateApplicationInput{OrganizationID: svcOrg, ProjectID: f.project.ID(), DisplayName: "API", DefaultLifecycleID: l.ID()})

		require.ErrorIs(t, err, svcErrBoom)
		f.requireUnchanged(t, state, events, resources)
	})
}

func TestServiceGetAndListApplications(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "l")
	api, _ := f.app(t, "api", l)
	worker, _ := f.app(t, "worker", l)
	_, err := f.svc.TransferApplication(svcCtx, svcAlice, svcOrg, worker.ID(), svcOtherTeam)
	require.NoError(t, err)
	_, other := f.addWorkspace(t, "data", "etl")
	f.appIn(t, other, "etl", f.lifecycleIn(t, other.WorkspaceID(), "l"))

	got, err := f.svc.GetApplication(svcCtx, svcAlice, svcOrg, api.ID())
	require.NoError(t, err)
	assert.Equal(t, api.Snapshot(), got.Snapshot())

	_, err = f.svc.GetApplication(svcCtx, svcAlice, svcOrg, "app_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.ErrorIs(t, err, ErrNotFound)

	page, err := f.svc.ListApplications(svcCtx, svcAlice, ListApplicationsQuery{OrganizationID: svcOrg, ProjectID: f.project.ID()})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)

	page, err = f.svc.ListApplications(svcCtx, svcAlice, ListApplicationsQuery{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), OwnerTeamID: svcOtherTeam})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, worker.ID(), page.Items[0].ID())
}

func TestServiceChangeApplication(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "l")
	app, _ := f.app(t, "api", l)
	f.app(t, "worker", l)

	t.Run("update", func(t *testing.T) {
		got, err := f.svc.UpdateApplication(svcCtx, svcAlice, svcOrg, app.ID(), UpdateApplicationProps{WorkloadSelector: map[string]string{"app": "api"}})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "api"}, got.WorkloadSelector())
		assert.Equal(t, EventApplicationUpdated, f.audit.last().Type)

		_, err = f.svc.UpdateApplication(svcCtx, svcAlice, svcOrg, app.ID(), UpdateApplicationProps{Slug: new("worker")})
		require.ErrorIs(t, err, ErrSlugTaken)
	})

	t.Run("link and unlink the source", func(t *testing.T) {
		got, err := f.svc.LinkApplicationSource(svcCtx, svcAlice, svcOrg, app.ID(), "src_9")
		require.NoError(t, err)
		assert.Equal(t, "src_9", *got.SourceID())

		got, err = f.svc.LinkApplicationSource(svcCtx, svcAlice, svcOrg, app.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.SourceID())
		assert.Equal(t, EventApplicationUpdated, f.audit.last().Type)
	})

	t.Run("transfer", func(t *testing.T) {
		got, err := f.svc.TransferApplication(svcCtx, svcAlice, svcOrg, app.ID(), svcOtherTeam)
		require.NoError(t, err)
		assert.Equal(t, svcOtherTeam, got.OwnerTeamID())
		ev := f.audit.last()
		assert.Equal(t, EventApplicationOwnerChanged, ev.Type)
		assert.Equal(t, svcTeam, ev.Before.(ApplicationSnapshot).OwnerTeamID)
		assert.Equal(t, svcOtherTeam, ev.After.(ApplicationSnapshot).OwnerTeamID)
	})

	t.Run("transfer to an inactive team", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.TransferApplication(svcCtx, svcAlice, svcOrg, app.ID(), svcGoneTeam)
		require.ErrorIs(t, err, ErrInactiveTeam)
		_, err = f.svc.TransferApplication(svcCtx, svcAlice, svcOrg, app.ID(), "")
		require.ErrorIs(t, err, ErrInvalid)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("archive", func(t *testing.T) {
		got, err := f.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, EventApplicationArchived, f.audit.last().Type)

		_, err = f.svc.UpdateApplication(svcCtx, svcAlice, svcOrg, app.ID(), UpdateApplicationProps{DisplayName: new("X")})
		require.ErrorIs(t, err, ErrArchived)
		_, err = f.svc.TransferApplication(svcCtx, svcAlice, svcOrg, app.ID(), svcTeam)
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("restore is refused in an archived project", func(t *testing.T) {
		f.archiveProject(f.project)
		state, events, resources := f.marks()
		_, err := f.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("restore", func(t *testing.T) {
		f.project.Restore()
		f.workspaces.putProject(f.project)
		got, err := f.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, EventApplicationRestored, f.audit.last().Type)
	})
}

func TestServiceCreateArtifact(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "l")
	app, _ := f.app(t, "api", l)
	reg := f.registry(t, "ghcr")
	other, _ := f.addWorkspace(t, "data", "etl")
	foreignReg := f.registryIn(t, other.ID(), "ghcr")
	oldReg := f.registry(t, "old")
	_, err := f.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, oldReg.ID())
	require.NoError(t, err)

	t.Run("created", func(t *testing.T) {
		a, err := f.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{
			OrganizationID: svcOrg, ApplicationID: app.ID(), Name: "api", ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/api", RegistryID: reg.ID(), Required: true,
		})
		require.NoError(t, err)
		assert.True(t, id.Valid(Artifact{}.Prefix(), a.ID()))
		assert.Equal(t, ArtifactDeclared, a.Source())
		assert.Equal(t, reg.ID(), *a.RegistryID())
		ev := f.audit.last()
		assert.Equal(t, EventArtifactCreated, ev.Type)
		assert.Equal(t, a.Snapshot(), ev.After)
		stored, err := f.store.GetArtifact(svcCtx, svcOrg, a.ID())
		require.NoError(t, err)
		assert.Equal(t, a.Snapshot(), stored.Snapshot())
	})

	tests := []struct {
		name       string
		artifact   string
		registryID string
		wantErr    error
	}{
		{"name taken", "api", "", ErrSlugTaken},
		{"registry in another workspace", "migrate", foreignReg.ID(), ErrWorkspaceMismatch},
		{"archived registry", "migrate", oldReg.ID(), ErrArchived},
		{"missing registry", "migrate", "reg_01ARZ3NDEKTSV4RRFFQ69G5FAW", ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, events, resources := f.marks()
			_, err := f.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{
				OrganizationID: svcOrg, ApplicationID: app.ID(), Name: tt.artifact, ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/x", RegistryID: tt.registryID,
			})
			require.ErrorIs(t, err, tt.wantErr)
			f.requireUnchanged(t, state, events, resources)
		})
	}

	t.Run("refused for an archived application", func(t *testing.T) {
		_, err := f.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)
		_, err = f.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{OrganizationID: svcOrg, ApplicationID: app.ID(), Name: "migrate", ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("refused for a missing application", func(t *testing.T) {
		_, err := f.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{OrganizationID: svcOrg, ApplicationID: "app_01ARZ3NDEKTSV4RRFFQ69G5FAW", Name: "migrate", ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestServiceChangeArtifact(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "l")
	app, _ := f.app(t, "api", l)
	a := f.artifact(t, app, "api")
	f.artifact(t, app, "worker")
	reg := f.registry(t, "ghcr")
	other, _ := f.addWorkspace(t, "data", "etl")
	foreignReg := f.registryIn(t, other.ID(), "ghcr")

	t.Run("list", func(t *testing.T) {
		got, err := f.svc.ListArtifacts(svcCtx, svcAlice, svcOrg, app.ID(), false)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("update", func(t *testing.T) {
		got, err := f.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, a.ID(), UpdateArtifactProps{ArtifactName: new("ghcr.io/acme/api-v2"), Required: new(true)})
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/api-v2", got.ArtifactName())
		assert.True(t, got.Required())
		assert.Equal(t, EventArtifactUpdated, f.audit.last().Type)

		_, err = f.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, a.ID(), UpdateArtifactProps{Name: new("worker")})
		require.ErrorIs(t, err, ErrSlugTaken)
	})

	t.Run("set and clear the registry", func(t *testing.T) {
		got, err := f.svc.SetArtifactRegistry(svcCtx, svcAlice, svcOrg, a.ID(), reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.ID(), *got.RegistryID())

		_, err = f.svc.SetArtifactRegistry(svcCtx, svcAlice, svcOrg, a.ID(), foreignReg.ID())
		require.ErrorIs(t, err, ErrWorkspaceMismatch)

		got, err = f.svc.SetArtifactRegistry(svcCtx, svcAlice, svcOrg, a.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.RegistryID())
	})

	t.Run("declare an observed artifact", func(t *testing.T) {
		observed, err := NewArtifact(NewArtifactProps{OrganizationID: svcOrg, ApplicationID: app.ID(), Name: "sidecar", ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/sidecar", Source: ArtifactObserved}, nil)
		require.NoError(t, err)
		require.NoError(t, f.store.CreateArtifact(svcCtx, observed))

		got, err := f.svc.DeclareArtifact(svcCtx, svcAlice, svcOrg, observed.ID())

		require.NoError(t, err)
		assert.Equal(t, ArtifactDeclared, got.Source())
		ev := f.audit.last()
		assert.Equal(t, EventArtifactUpdated, ev.Type)
		assert.Equal(t, ArtifactObserved, ev.Before.(ArtifactSnapshot).Source)
	})

	t.Run("archive and restore", func(t *testing.T) {
		got, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, EventArtifactArchived, f.audit.last().Type)

		got, err = f.svc.RestoreArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, EventArtifactRestored, f.audit.last().Type)
	})

	t.Run("restore is refused for an archived application", func(t *testing.T) {
		_, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.NoError(t, err)
		_, err = f.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)

		_, err = f.svc.RestoreArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.ErrorIs(t, err, ErrArchived)
	})
}

func TestServiceArtifactsInUse(t *testing.T) {
	t.Run("archive is refused while an active channel rule is scoped to it", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "l")
		app, _ := f.app(t, "api", l)
		a := f.artifact(t, app, "api")
		ch := f.channel(t, app, l, "hotfix", svcGitRule(new(a.ID())))
		state, events, resources := f.marks()

		_, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)

		_, err = f.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, ch.ID())
		require.NoError(t, err)
		_, err = f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, a.ID())
		require.NoError(t, err, "rules of archived channels do not hold the artifact")
	})

	t.Run("archive and rename are refused while a placement applies releases from it", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		tg := f.target(t, e, "web")
		l := f.lifecycle(t, "l", e)
		app, _ := f.app(t, "api", l)
		chart := f.artifact(t, app, "chart")
		f.placement(t, app, tg, &ApplyConfig{DefinitionArtifact: "chart"})
		state, events, resources := f.marks()

		_, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, chart.ID())
		require.ErrorIs(t, err, ErrInUse)
		_, err = f.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, chart.ID(), UpdateArtifactProps{Name: new("helm")})
		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)

		_, err = f.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, chart.ID(), UpdateArtifactProps{Name: new("chart"), Required: new(true)})
		require.NoError(t, err, "keeping the name is not a rename")

		_, err = f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), nil)
		require.NoError(t, err)
		_, err = f.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, chart.ID(), UpdateArtifactProps{Name: new("helm")})
		require.NoError(t, err)
	})
}

func TestServiceCreatePlacement(t *testing.T) {
	setup := func(t *testing.T) (*svcFixture, *Application, *Target) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		tg := f.target(t, e, "web")
		l := f.lifecycle(t, "l", e)
		app, _ := f.app(t, "api", l)
		f.artifact(t, app, "chart")
		return f, app, tg
	}

	t.Run("created", func(t *testing.T) {
		f, app, tg := setup(t)

		p, err := f.svc.CreatePlacement(svcCtx, svcAlice, CreatePlacementInput{OrganizationID: svcOrg, ApplicationID: app.ID(), TargetID: tg.ID(), ApplyConfig: &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"crd_values"}}})

		require.NoError(t, err)
		assert.Equal(t, PlacementConfigured, p.Source())
		assert.Equal(t, &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"crd_values"}}, p.ApplyConfig())
		ev := f.audit.last()
		assert.Equal(t, EventPlacementCreated, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID)
		assert.Equal(t, p.Snapshot(), ev.After)
		stored, err := f.store.GetPlacement(svcCtx, svcOrg, app.ID(), tg.ID())
		require.NoError(t, err)
		assert.Equal(t, p.Snapshot(), stored.Snapshot())
	})

	t.Run("tracked only", func(t *testing.T) {
		f, app, tg := setup(t)
		p := f.placement(t, app, tg, nil)
		assert.Nil(t, p.ApplyConfig())
	})

	refusals := []struct {
		name    string
		prepare func(*testing.T, *svcFixture, *Application, *Target) (string, *ApplyConfig)
		wantErr error
	}{
		{"duplicate", func(t *testing.T, f *svcFixture, app *Application, tg *Target) (string, *ApplyConfig) {
			f.placement(t, app, tg, nil)
			return tg.ID(), nil
		}, ErrAlreadyExists},
		{"target in another workspace", func(t *testing.T, f *svcFixture, _ *Application, _ *Target) (string, *ApplyConfig) {
			other, _ := f.addWorkspace(t, "data", "etl")
			return f.target(t, f.envIn(t, other.ID(), "prod"), "web").ID(), nil
		}, ErrWorkspaceMismatch},
		{"archived target", func(t *testing.T, f *svcFixture, _ *Application, tg *Target) (string, *ApplyConfig) {
			_, err := f.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, tg.ID())
			require.NoError(t, err)
			return tg.ID(), nil
		}, ErrArchived},
		{"missing target", func(*testing.T, *svcFixture, *Application, *Target) (string, *ApplyConfig) {
			return "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAW", nil
		}, ErrNotFound},
		{"unknown definition artifact", func(_ *testing.T, _ *svcFixture, _ *Application, tg *Target) (string, *ApplyConfig) {
			return tg.ID(), &ApplyConfig{DefinitionArtifact: "helm"}
		}, ErrNotFound},
		{"archived definition artifact", func(t *testing.T, f *svcFixture, app *Application, tg *Target) (string, *ApplyConfig) {
			a := f.artifact(t, app, "old-chart")
			_, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, a.ID())
			require.NoError(t, err)
			return tg.ID(), &ApplyConfig{DefinitionArtifact: "old-chart"}
		}, ErrNotFound},
		{"definition artifact of another application", func(t *testing.T, f *svcFixture, _ *Application, tg *Target) (string, *ApplyConfig) {
			other, _ := f.app(t, "worker", f.lifecycle(t, "l2"))
			f.artifact(t, other, "worker-chart")
			return tg.ID(), &ApplyConfig{DefinitionArtifact: "worker-chart"}
		}, ErrNotFound},
		{"archived application", func(t *testing.T, f *svcFixture, app *Application, tg *Target) (string, *ApplyConfig) {
			_, err := f.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
			require.NoError(t, err)
			return tg.ID(), nil
		}, ErrArchived},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			f, app, tg := setup(t)
			targetID, config := tt.prepare(t, f, app, tg)
			state, events, resources := f.marks()

			_, err := f.svc.CreatePlacement(svcCtx, svcAlice, CreatePlacementInput{OrganizationID: svcOrg, ApplicationID: app.ID(), TargetID: targetID, ApplyConfig: config})

			require.ErrorIs(t, err, tt.wantErr)
			f.requireUnchanged(t, state, events, resources)
		})
	}
}

func TestServiceConfigureAndRemovePlacement(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	tg := f.target(t, e, "web")
	l := f.lifecycle(t, "l", e)
	app, _ := f.app(t, "api", l)
	f.artifact(t, app, "chart")
	f.artifact(t, app, "helm")
	f.placement(t, app, tg, &ApplyConfig{DefinitionArtifact: "chart"})

	t.Run("list", func(t *testing.T) {
		got, err := f.svc.ListPlacements(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, tg.ID(), got[0].TargetID())
	})

	t.Run("configure", func(t *testing.T) {
		got, err := f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), &ApplyConfig{DefinitionArtifact: "helm"})
		require.NoError(t, err)
		assert.Equal(t, "helm", got.ApplyConfig().DefinitionArtifact)
		ev := f.audit.last()
		assert.Equal(t, EventPlacementUpdated, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID)
		assert.Equal(t, "chart", ev.Before.(PlacementSnapshot).ApplyConfig.DefinitionArtifact)
	})

	t.Run("configure checks the definition artifact", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), &ApplyConfig{DefinitionArtifact: "nope"})
		require.ErrorIs(t, err, ErrNotFound)
		_, err = f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{""}})
		require.ErrorIs(t, err, ErrInvalid)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("nil clears", func(t *testing.T) {
		got, err := f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), nil)
		require.NoError(t, err)
		assert.Nil(t, got.ApplyConfig())
		stored, err := f.store.GetPlacement(svcCtx, svcOrg, app.ID(), tg.ID())
		require.NoError(t, err)
		assert.Nil(t, stored.ApplyConfig())
	})

	t.Run("configure a missing placement", func(t *testing.T) {
		_, err := f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAW", nil)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("remove is refused while deployments refer to it", func(t *testing.T) {
		f.store.inUse[fakePlacementKey(app.ID(), tg.ID())] = true
		t.Cleanup(func() { delete(f.store.inUse, fakePlacementKey(app.ID(), tg.ID())) })
		state, events, resources := f.marks()

		err := f.svc.RemovePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID())

		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("remove", func(t *testing.T) {
		before, err := f.store.GetPlacement(svcCtx, svcOrg, app.ID(), tg.ID())
		require.NoError(t, err)

		require.NoError(t, f.svc.RemovePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID()))

		_, err = f.store.GetPlacement(svcCtx, svcOrg, app.ID(), tg.ID())
		require.ErrorIs(t, err, ErrNotFound)
		ev := f.audit.last()
		assert.Equal(t, EventPlacementRemoved, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID)
		assert.Equal(t, before.Snapshot(), ev.Before)
		assert.Nil(t, ev.After)
	})

	t.Run("remove a missing placement", func(t *testing.T) {
		err := f.svc.RemovePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID())
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("configure is refused for an archived application", func(t *testing.T) {
		f.placement(t, app, tg, nil)
		_, err := f.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
		require.NoError(t, err)
		_, err = f.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, app.ID(), tg.ID(), &ApplyConfig{DefinitionArtifact: "chart"})
		require.ErrorIs(t, err, ErrArchived)
	})
}
