package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestServiceCreateLifecycle(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		f := newSvcFixture(t)
		staging := f.env(t, "staging")
		prod := f.env(t, "prod")

		l, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "Mainline", Description: "The usual path",
			Phases: []LifecyclePhaseProps{
				{Name: "Staging", EnvironmentIDs: []string{staging.ID()}},
				{Name: "Production", EnvironmentIDs: []string{prod.ID()}},
			},
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Lifecycle{}.Prefix(), l.ID()))
		assert.Equal(t, "mainline", l.Slug())
		assert.Equal(t, []string{staging.ID(), prod.ID()}, l.EnvironmentIDs())
		ev := f.audit.last()
		assert.Equal(t, EventLifecycleCreated, ev.Type)
		assert.Equal(t, l.Snapshot(), ev.After)
		stored, err := f.store.GetLifecycle(svcCtx, svcOrg, l.ID())
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), stored.Snapshot(), "phases are stored with their IDs and order")
	})

	t.Run("without phases", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "empty")
		assert.Empty(t, l.Phases())
	})

	refusals := []struct {
		name    string
		envID   func(*svcFixture) string
		wantErr error
	}{
		{"missing environment", func(*svcFixture) string { return "env_01ARZ3NDEKTSV4RRFFQ69G5FAW" }, ErrNotFound},
		{"archived environment", func(f *svcFixture) string {
			e := f.env(t, "old")
			_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
			require.NoError(t, err)
			return e.ID()
		}, ErrArchived},
		{"environment in another workspace", func(f *svcFixture) string {
			other, _ := f.addWorkspace(t, "data", "etl")
			return f.envIn(t, other.ID(), "staging").ID()
		}, ErrWorkspaceMismatch},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			f := newSvcFixture(t)
			good := f.env(t, "good")
			envID := tt.envID(f)
			state, events, resources := f.marks()

			_, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{
				OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "L",
				Phases: []LifecyclePhaseProps{{Name: "one", EnvironmentIDs: []string{good.ID()}}, {Name: "two", EnvironmentIDs: []string{envID}}},
			})

			require.ErrorIs(t, err, tt.wantErr)
			f.requireUnchanged(t, state, events, resources)
		})
	}

	t.Run("archived workspace", func(t *testing.T) {
		f := newSvcFixture(t)
		f.archiveWorkspace(t, f.ws)
		_, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "L"})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("slug taken", func(t *testing.T) {
		f := newSvcFixture(t)
		f.lifecycle(t, "mainline")
		_, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), Slug: "mainline", DisplayName: "L"})
		require.ErrorIs(t, err, ErrSlugTaken)
	})

	t.Run("an environment in two phases", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		_, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "L",
			Phases: []LifecyclePhaseProps{{Name: "a", EnvironmentIDs: []string{e.ID()}}, {Name: "b", EnvironmentIDs: []string{e.ID()}}},
		})
		require.ErrorIs(t, err, ErrInvalid)
	})
}

func TestServiceGetAndListLifecycles(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "mainline")
	f.lifecycle(t, "hotfix")

	got, err := f.svc.GetLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
	require.NoError(t, err)
	assert.Equal(t, l.Snapshot(), got.Snapshot())

	_, err = f.svc.GetLifecycle(svcCtx, svcAlice, svcOrg, "lfc_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.ErrorIs(t, err, ErrNotFound)

	page, err := f.svc.ListLifecycles(svcCtx, svcAlice, ListLifecyclesQuery{OrganizationID: svcOrg, WorkspaceID: f.ws.ID()})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)
}

func TestServiceLifecyclePhases(t *testing.T) {
	f := newSvcFixture(t)
	staging := f.env(t, "staging")
	prod := f.env(t, "prod")
	qa := f.env(t, "qa")
	l := f.lifecycle(t, "mainline", staging)
	stagingPhase := l.Phases()[0].ID

	t.Run("update the lifecycle", func(t *testing.T) {
		got, err := f.svc.UpdateLifecycle(svcCtx, svcAlice, svcOrg, l.ID(), UpdateLifecycleProps{Description: new("Main path")})
		require.NoError(t, err)
		assert.Equal(t, "Main path", *got.Description())
		assert.Equal(t, EventLifecycleUpdated, f.audit.last().Type)
	})

	t.Run("add", func(t *testing.T) {
		got, err := f.svc.AddLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), LifecyclePhaseProps{Name: "prod", EnvironmentIDs: []string{prod.ID()}})
		require.NoError(t, err)
		assert.Equal(t, []string{staging.ID(), prod.ID()}, got.EnvironmentIDs())
		ev := f.audit.last()
		assert.Equal(t, EventLifecycleUpdated, ev.Type)
		assert.Len(t, ev.Before.(LifecycleSnapshot).Phases, 1)
		assert.Len(t, ev.After.(LifecycleSnapshot).Phases, 2)
	})

	t.Run("add checks the environments", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.AddLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), LifecyclePhaseProps{Name: "nope", EnvironmentIDs: []string{"env_01ARZ3NDEKTSV4RRFFQ69G5FAW"}})
		require.ErrorIs(t, err, ErrNotFound)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("update checks the environments", func(t *testing.T) {
		other, _ := f.addWorkspace(t, "data", "etl")
		foreign := f.envIn(t, other.ID(), "foreign")
		_, err := f.svc.UpdateLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), stagingPhase, LifecyclePhaseProps{Name: "staging", EnvironmentIDs: []string{foreign.ID()}})
		require.ErrorIs(t, err, ErrWorkspaceMismatch)
	})

	t.Run("update", func(t *testing.T) {
		got, err := f.svc.UpdateLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), stagingPhase, LifecyclePhaseProps{Name: "pre-prod", EnvironmentIDs: []string{staging.ID(), qa.ID()}, MinEnvironments: new(1)})
		require.NoError(t, err)
		p := got.Phases()[0]
		assert.Equal(t, stagingPhase, p.ID, "the phase keeps its ID")
		assert.Equal(t, "pre-prod", p.Name)
		assert.ElementsMatch(t, []string{staging.ID(), qa.ID()}, p.EnvironmentIDs)
	})

	t.Run("move", func(t *testing.T) {
		got, err := f.svc.MoveLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), stagingPhase, 1)
		require.NoError(t, err)
		assert.Equal(t, stagingPhase, got.Phases()[1].ID)
		stored, err := f.store.GetLifecycle(svcCtx, svcOrg, l.ID())
		require.NoError(t, err)
		assert.Equal(t, got.Snapshot(), stored.Snapshot())

		_, err = f.svc.MoveLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), stagingPhase, 5)
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("an unknown phase", func(t *testing.T) {
		_, err := f.svc.RemoveLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), "lfp_01ARZ3NDEKTSV4RRFFQ69G5FAW")
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("remove", func(t *testing.T) {
		got, err := f.svc.RemoveLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), stagingPhase)
		require.NoError(t, err)
		assert.Equal(t, []string{prod.ID()}, got.EnvironmentIDs())

		_, err = f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{staging.ID()})
		require.NoError(t, err, "an environment removed from every lifecycle can be archived")
	})
}

func TestServiceArchiveLifecycle(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	l := f.lifecycle(t, "mainline", e)
	app, _ := f.app(t, "api", l)
	other := f.lifecycle(t, "other")
	hotfix := f.channel(t, app, l, "hotfix")

	t.Run("refused while an active channel follows it", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)
	})

	_, err := f.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, hotfix.ID())
	require.NoError(t, err)
	_, err = f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
	require.ErrorIs(t, err, ErrInUse, "the default channel still follows it")

	_, err = f.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, func() string {
		chs, err := f.svc.ListChannels(svcCtx, svcAlice, svcOrg, app.ID(), false)
		require.NoError(t, err)
		require.Len(t, chs, 1)
		return chs[0].ID()
	}(), other.ID())
	require.NoError(t, err)

	got, err := f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
	require.NoError(t, err, "only archived channels follow it now")
	assert.True(t, got.Archived())
	assert.Equal(t, EventLifecycleArchived, f.audit.last().Type)

	t.Run("an archived lifecycle cannot change", func(t *testing.T) {
		_, err := f.svc.AddLifecyclePhase(svcCtx, svcAlice, svcOrg, l.ID(), LifecyclePhaseProps{Name: "x", EnvironmentIDs: []string{e.ID()}})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("an archived lifecycle frees its environments", func(t *testing.T) {
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
	})

	t.Run("restore is refused while an environment in it is archived", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("restored once its environments are active", func(t *testing.T) {
		_, err := f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)

		got, err := f.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, l.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		ev := f.audit.last()
		assert.Equal(t, EventLifecycleRestored, ev.Type)
		assert.NotNil(t, ev.Before.(LifecycleSnapshot).DeletedAt)
	})

	t.Run("restore is refused in an archived workspace", func(t *testing.T) {
		_, err := f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, other.ID())
		require.ErrorIs(t, err, ErrInUse, "the default channel follows other")
		empty := f.lifecycle(t, "empty")
		_, err = f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, empty.ID())
		require.NoError(t, err)
		f.archiveWorkspace(t, f.ws)

		_, err = f.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, empty.ID())
		require.ErrorIs(t, err, ErrArchived)
	})
}
