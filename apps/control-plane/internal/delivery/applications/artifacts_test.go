package applications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestCreateArtifact(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "l", false)
	app, _ := w.app(t, prjActive, "api", l)
	reg := w.registry(t, wsActive, "ghcr", false)
	foreignReg := w.registry(t, wsOther, "ghcr", false)
	oldReg := w.registry(t, wsActive, "old", true)

	t.Run("created", func(t *testing.T) {
		w.reset()
		a, err := w.svc.CreateArtifact(ctx, ada, delivery.CreateArtifactInput{
			OrganizationID: org, ApplicationID: app.ID(), Name: "api", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/api", RegistryID: reg.ID(), Required: true,
		})
		require.NoError(t, err)
		assert.True(t, id.Valid(delivery.Artifact{}.Prefix(), a.ID()))
		assert.Equal(t, delivery.ArtifactDeclared, a.Source())
		assert.Equal(t, reg.ID(), *a.RegistryID())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventArtifactCreated, ev.Type)
		assert.Equal(t, a.Snapshot(), ev.After)
		assert.Equal(t, "dec-"+prjActive, ev.DecisionID)
		assert.Equal(t, a.Snapshot(), w.artifacts[a.ID()])
		assert.Equal(t, []string{"delivery.v1.artifact.created"}, w.eventTypes())
	})

	refusals := []struct {
		name       string
		artifact   string
		registryID string
		wantErr    error
	}{
		{"name taken", "api", "", delivery.ErrSlugTaken},
		{"registry in another workspace", "migrate", foreignReg.ID(), delivery.ErrWorkspaceMismatch},
		{"archived registry", "migrate", oldReg.ID(), delivery.ErrArchived},
		{"missing registry", "migrate", "reg_01JZ0000000000000000000099", delivery.ErrNotFound},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			before := w.state.clone()
			_, err := w.svc.CreateArtifact(ctx, ada, delivery.CreateArtifactInput{
				OrganizationID: org, ApplicationID: app.ID(), Name: tt.artifact, ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/x", RegistryID: tt.registryID,
			})
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, w.state)
		})
	}

	t.Run("refused for an archived application", func(t *testing.T) {
		w.archiveApp(t, app)
		_, err := w.svc.CreateArtifact(ctx, ada, delivery.CreateArtifactInput{OrganizationID: org, ApplicationID: app.ID(), Name: "migrate", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
		require.ErrorIs(t, err, delivery.ErrArchived)
	})

	t.Run("refused for a missing application", func(t *testing.T) {
		_, err := w.svc.CreateArtifact(ctx, ada, delivery.CreateArtifactInput{OrganizationID: org, ApplicationID: missingApp, Name: "migrate", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})
}

func TestChangeArtifact(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "l", false)
	app, _ := w.app(t, prjActive, "api", l)
	a := w.artifact(t, app, "api")
	w.artifact(t, app, "worker")
	reg := w.registry(t, wsActive, "ghcr", false)
	foreignReg := w.registry(t, wsOther, "ghcr", false)

	t.Run("list", func(t *testing.T) {
		got, err := w.svc.ListArtifacts(ctx, ada, org, app.ID(), false)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("update", func(t *testing.T) {
		got, err := w.svc.UpdateArtifact(ctx, ada, org, a.ID(), delivery.UpdateArtifactProps{ArtifactName: ptr("ghcr.io/acme/api-v2"), Required: ptr(true)})
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/api-v2", got.ArtifactName())
		assert.True(t, got.Required())
		assert.Equal(t, delivery.EventArtifactUpdated, w.lastAudit().Type)

		_, err = w.svc.UpdateArtifact(ctx, ada, org, a.ID(), delivery.UpdateArtifactProps{Name: ptr("worker")})
		require.ErrorIs(t, err, delivery.ErrSlugTaken)
	})

	t.Run("set and clear the registry", func(t *testing.T) {
		got, err := w.svc.SetArtifactRegistry(ctx, ada, org, a.ID(), reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.ID(), *got.RegistryID())

		_, err = w.svc.SetArtifactRegistry(ctx, ada, org, a.ID(), foreignReg.ID())
		require.ErrorIs(t, err, delivery.ErrWorkspaceMismatch)

		got, err = w.svc.SetArtifactRegistry(ctx, ada, org, a.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.RegistryID())
	})

	t.Run("declare an observed artifact", func(t *testing.T) {
		observed := must(delivery.NewArtifact(delivery.NewArtifactProps{OrganizationID: org, ApplicationID: app.ID(), Name: "sidecar", ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/sidecar", Source: delivery.ArtifactObserved}, nil))
		require.NoError(t, w.CreateArtifact(ctx, observed))

		got, err := w.svc.DeclareArtifact(ctx, ada, org, observed.ID())

		require.NoError(t, err)
		assert.Equal(t, delivery.ArtifactDeclared, got.Source())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventArtifactUpdated, ev.Type)
		assert.Equal(t, delivery.ArtifactObserved, ev.Before.(delivery.ArtifactSnapshot).Source)
	})

	t.Run("archive and restore", func(t *testing.T) {
		got, err := w.svc.ArchiveArtifact(ctx, ada, org, a.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, delivery.EventArtifactArchived, w.lastAudit().Type)

		got, err = w.svc.RestoreArtifact(ctx, ada, org, a.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, delivery.EventArtifactRestored, w.lastAudit().Type)

		n := len(w.audit)
		_, err = w.svc.RestoreArtifact(ctx, ada, org, a.ID())
		require.NoError(t, err)
		assert.Len(t, w.audit, n, "restoring an active artifact records nothing")
	})

	t.Run("restore is refused for an archived application", func(t *testing.T) {
		_, err := w.svc.ArchiveArtifact(ctx, ada, org, a.ID())
		require.NoError(t, err)
		w.archiveApp(t, app)

		_, err = w.svc.RestoreArtifact(ctx, ada, org, a.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
	})
}

func TestArtifactsInUse(t *testing.T) {
	ctx := context.Background()

	t.Run("archive is refused while an active channel rule is scoped to it", func(t *testing.T) {
		w := newWorld(t)
		l := w.lifecycle(t, wsActive, "l", false)
		app, _ := w.app(t, prjActive, "api", l)
		a := w.artifact(t, app, "api")
		ch := w.channel(t, app, l, "hotfix", gitRule(ptr(a.ID())))
		before := w.state.clone()

		_, err := w.svc.ArchiveArtifact(ctx, ada, org, a.ID())
		require.ErrorIs(t, err, delivery.ErrInUse)
		assert.Equal(t, before, w.state)

		_, err = w.svc.ArchiveChannel(ctx, ada, org, ch.ID())
		require.NoError(t, err)
		_, err = w.svc.ArchiveArtifact(ctx, ada, org, a.ID())
		require.NoError(t, err, "rules of archived channels do not hold the artifact")
	})

	t.Run("archive and rename are refused while a placement applies releases from it", func(t *testing.T) {
		w := newWorld(t)
		tg := w.target(t, w.env(t, wsActive, "staging"), "web")
		l := w.lifecycle(t, wsActive, "l", false)
		app, _ := w.app(t, prjActive, "api", l)
		chart := w.artifact(t, app, "chart")
		w.placement(t, app, tg, &delivery.ApplyConfig{DefinitionArtifact: "chart"})
		before := w.state.clone()

		_, err := w.svc.ArchiveArtifact(ctx, ada, org, chart.ID())
		require.ErrorIs(t, err, delivery.ErrInUse)
		_, err = w.svc.UpdateArtifact(ctx, ada, org, chart.ID(), delivery.UpdateArtifactProps{Name: ptr("helm")})
		require.ErrorIs(t, err, delivery.ErrInUse)
		assert.Equal(t, before, w.state)

		_, err = w.svc.UpdateArtifact(ctx, ada, org, chart.ID(), delivery.UpdateArtifactProps{Name: ptr("chart"), Required: ptr(true)})
		require.NoError(t, err, "keeping the name is not a rename")

		_, err = w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), nil)
		require.NoError(t, err)
		_, err = w.svc.UpdateArtifact(ctx, ada, org, chart.ID(), delivery.UpdateArtifactProps{Name: ptr("helm")})
		require.NoError(t, err)
	})
}

// An archived Application's artifacts and Channels change only once it is
// restored.
func TestArchivedApplicationIsReadOnly(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "l", false)
	app, defaultCh := w.app(t, prjActive, "api", l)
	a := w.artifact(t, app, "api")
	w.archiveApp(t, app)
	before := w.state.clone()

	_, err := w.svc.UpdateChannel(ctx, ada, org, defaultCh.ID(), delivery.UpdateChannelProps{DisplayName: ptr("Renamed")})
	assert.ErrorIs(t, err, delivery.ErrArchived)
	_, err = w.svc.AddChannelRule(ctx, ada, org, defaultCh.ID(), gitRule(nil))
	assert.ErrorIs(t, err, delivery.ErrArchived)
	_, err = w.svc.UpdateArtifact(ctx, ada, org, a.ID(), delivery.UpdateArtifactProps{Required: ptr(false)})
	assert.ErrorIs(t, err, delivery.ErrArchived)
	_, err = w.svc.DeclareArtifact(ctx, ada, org, a.ID())
	assert.ErrorIs(t, err, delivery.ErrArchived)
	assert.Equal(t, before, w.state)
}
