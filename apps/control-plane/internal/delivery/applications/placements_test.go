package applications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// placementSetup is an Application with a "chart" artifact and a Target in
// its Workspace.
func placementSetup(t *testing.T) (*world, *delivery.Application, *delivery.Target) {
	t.Helper()
	w := newWorld(t)
	tg := w.target(t, w.env(t, wsActive, "staging"), "web")
	l := w.lifecycle(t, wsActive, "l", false)
	app, _ := w.app(t, prjActive, "api", l)
	w.artifact(t, app, "chart")
	w.reset()
	return w, app, tg
}

func TestCreatePlacement(t *testing.T) {
	ctx := context.Background()

	t.Run("created", func(t *testing.T) {
		w, app, tg := placementSetup(t)
		config := &delivery.ApplyConfig{DefinitionArtifact: "chart", Values: []string{"crd_values"}}

		p, err := w.svc.CreatePlacement(ctx, ada, delivery.CreatePlacementInput{OrganizationID: org, ApplicationID: app.ID(), TargetID: tg.ID(), ApplyConfig: config})

		require.NoError(t, err)
		assert.Equal(t, delivery.PlacementConfigured, p.Source())
		assert.Equal(t, config, p.ApplyConfig())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventPlacementCreated, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID, "a placement's record names its application")
		assert.Equal(t, p.Snapshot(), ev.After)
		assert.Equal(t, "dec-"+prjActive, ev.DecisionID)
		assert.Equal(t, p.Snapshot(), w.placements[placementKey(app.ID(), tg.ID())])
		require.Len(t, w.events, 1)
		assert.Equal(t, "delivery.v1.placement.created", w.events[0].Type)
		assert.Equal(t, delivery.AggregatePlacement, w.events[0].AggregateType)
		assert.Equal(t, app.ID()+":"+tg.ID(), w.events[0].AggregateID, "a placement is identified by its application and target")
	})

	t.Run("tracked only", func(t *testing.T) {
		w, app, tg := placementSetup(t)
		p := w.placement(t, app, tg, nil)
		assert.Nil(t, p.ApplyConfig())
	})

	refusals := []struct {
		name    string
		prepare func(*testing.T, *world, *delivery.Application, *delivery.Target) (string, *delivery.ApplyConfig)
		wantErr error
	}{
		{"duplicate", func(t *testing.T, w *world, app *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			w.placement(t, app, tg, nil)
			return tg.ID(), nil
		}, delivery.ErrAlreadyExists},
		{"target in another workspace", func(t *testing.T, w *world, _ *delivery.Application, _ *delivery.Target) (string, *delivery.ApplyConfig) {
			return w.target(t, w.env(t, wsOther, "prod"), "web").ID(), nil
		}, delivery.ErrWorkspaceMismatch},
		{"archived target", func(_ *testing.T, w *world, _ *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			tg.Archive()
			w.targets[tg.ID()] = tg.Snapshot()
			return tg.ID(), nil
		}, delivery.ErrArchived},
		{"archived environment", func(t *testing.T, w *world, _ *delivery.Application, _ *delivery.Target) (string, *delivery.ApplyConfig) {
			e := w.env(t, wsActive, "old")
			tg := w.target(t, e, "web")
			e.Archive()
			w.environments[e.ID()] = e.Snapshot()
			return tg.ID(), nil
		}, delivery.ErrArchived},
		{"missing target", func(*testing.T, *world, *delivery.Application, *delivery.Target) (string, *delivery.ApplyConfig) {
			return "tgt_01JZ0000000000000000000099", nil
		}, delivery.ErrNotFound},
		{"unknown definition artifact", func(_ *testing.T, _ *world, _ *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			return tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "helm"}
		}, delivery.ErrNotFound},
		{"archived definition artifact", func(t *testing.T, w *world, app *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			a := w.artifact(t, app, "old-chart")
			_, err := w.svc.ArchiveArtifact(context.Background(), ada, org, a.ID())
			require.NoError(t, err)
			return tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "old-chart"}
		}, delivery.ErrNotFound},
		{"definition artifact of another application", func(t *testing.T, w *world, _ *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			other, _ := w.app(t, prjActive, "worker", w.lifecycle(t, wsActive, "l2", false))
			w.artifact(t, other, "worker-chart")
			return tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "worker-chart"}
		}, delivery.ErrNotFound},
		{"archived application", func(t *testing.T, w *world, app *delivery.Application, tg *delivery.Target) (string, *delivery.ApplyConfig) {
			w.archiveApp(t, app)
			return tg.ID(), nil
		}, delivery.ErrArchived},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			w, app, tg := placementSetup(t)
			targetID, config := tt.prepare(t, w, app, tg)
			before := w.state.clone()

			_, err := w.svc.CreatePlacement(ctx, ada, delivery.CreatePlacementInput{OrganizationID: org, ApplicationID: app.ID(), TargetID: targetID, ApplyConfig: config})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, w.state)
		})
	}
}

func TestConfigureAndRemovePlacement(t *testing.T) {
	ctx := context.Background()
	w, app, tg := placementSetup(t)
	w.artifact(t, app, "helm")
	w.placement(t, app, tg, &delivery.ApplyConfig{DefinitionArtifact: "chart"})

	t.Run("list", func(t *testing.T) {
		got, err := w.svc.ListPlacements(ctx, ada, org, app.ID())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, tg.ID(), got[0].TargetID())
	})

	t.Run("configure", func(t *testing.T) {
		got, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "helm"})
		require.NoError(t, err)
		assert.Equal(t, "helm", got.ApplyConfig().DefinitionArtifact)
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventPlacementUpdated, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID)
		assert.Equal(t, "chart", ev.Before.(delivery.PlacementSnapshot).ApplyConfig.DefinitionArtifact)
	})

	t.Run("configure checks the definition artifact", func(t *testing.T) {
		before := w.state.clone()
		_, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "nope"})
		require.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "chart", Values: []string{""}})
		require.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Equal(t, before, w.state)
	})

	t.Run("an invalid config is refused before asking", func(t *testing.T) {
		w.reset()
		_, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), id.New(delivery.Target{}.Prefix()), &delivery.ApplyConfig{DefinitionArtifact: "Not A Name"})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Empty(t, w.authz.asked)
	})

	t.Run("nil clears", func(t *testing.T) {
		got, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), nil)
		require.NoError(t, err)
		assert.Nil(t, got.ApplyConfig())
		assert.Nil(t, w.placements[placementKey(app.ID(), tg.ID())].ApplyConfig)

		w.reset()
		_, err = w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), nil)
		require.NoError(t, err)
		assert.Empty(t, w.audit, "clearing nothing records nothing")
	})

	t.Run("configure a missing placement", func(t *testing.T) {
		_, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), "tgt_01JZ0000000000000000000099", nil)
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("remove is refused while deployments refer to it", func(t *testing.T) {
		k := placementKey(app.ID(), tg.ID())
		w.inUse[k] = true
		t.Cleanup(func() { delete(w.inUse, k) })
		before := w.state.clone()

		err := w.svc.RemovePlacement(ctx, ada, org, app.ID(), tg.ID())

		require.ErrorIs(t, err, delivery.ErrInUse)
		assert.Equal(t, before, w.state)
	})

	t.Run("remove", func(t *testing.T) {
		before := w.placements[placementKey(app.ID(), tg.ID())]
		w.reset()

		require.NoError(t, w.svc.RemovePlacement(ctx, ada, org, app.ID(), tg.ID()))

		assert.NotContains(t, w.placements, placementKey(app.ID(), tg.ID()))
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventPlacementRemoved, ev.Type)
		assert.Equal(t, app.ID(), ev.ResourceID)
		assert.Equal(t, before, ev.Before)
		assert.Nil(t, ev.After)
		require.Len(t, w.events, 1)
		assert.Equal(t, "delivery.v1.placement.removed", w.events[0].Type)
		assert.Equal(t, app.ID()+":"+tg.ID(), w.events[0].AggregateID)
		assert.Contains(t, string(w.events[0].Payload), `"target_id":"`+tg.ID()+`"`)
	})

	t.Run("remove a missing placement", func(t *testing.T) {
		err := w.svc.RemovePlacement(ctx, ada, org, app.ID(), tg.ID())
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("configure is refused for an archived application", func(t *testing.T) {
		w.placement(t, app, tg, nil)
		w.archiveApp(t, app)
		_, err := w.svc.ConfigurePlacement(ctx, ada, org, app.ID(), tg.ID(), &delivery.ApplyConfig{DefinitionArtifact: "chart"})
		require.ErrorIs(t, err, delivery.ErrArchived)
	})
}
