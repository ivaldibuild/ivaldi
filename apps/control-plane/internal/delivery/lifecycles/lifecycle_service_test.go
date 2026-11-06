package lifecycles

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

func ptr[T any](v T) *T { return &v }

func TestNewLifecycleServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]LifecycleServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewLifecycleService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewLifecycleService(all...)
	assert.NoError(t, err)
}

func TestCreateLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("created", func(t *testing.T) {
		w := newWorld(t)
		staging := w.env(t, wsActive, "staging", false)
		prod := w.env(t, wsActive, "prod", false)

		l, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{
			OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Mainline", Description: "The usual path",
			Phases: []delivery.LifecyclePhaseProps{
				{Name: "Staging", EnvironmentIDs: []string{staging.ID()}},
				{Name: "Production", EnvironmentIDs: []string{prod.ID()}},
			},
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(delivery.Lifecycle{}.Prefix(), l.ID()))
		assert.Equal(t, "mainline", l.Slug())
		assert.Equal(t, []string{staging.ID(), prod.ID()}, l.EnvironmentIDs())
		assert.Equal(t, l.Snapshot(), w.lifecycles[l.ID()], "phases are stored with their IDs and order")
		require.Len(t, w.audit, 1)
		assert.Equal(t, delivery.EventLifecycleCreated, w.audit[0].Type)
		assert.Equal(t, l.Snapshot(), w.audit[0].After)
		assert.Equal(t, ada, w.audit[0].Actor)
		assert.Equal(t, "dec-"+wsActive, w.audit[0].DecisionID)
		assert.Equal(t, []string{"delivery.v1.lifecycle.created"}, w.eventTypes())
		assert.Equal(t, delivery.AggregateLifecycle, w.events[0].AggregateType)
		assert.Equal(t, l.ID(), w.events[0].AggregateID)
	})

	t.Run("without phases", func(t *testing.T) {
		w := newWorld(t)
		l, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Empty"})
		require.NoError(t, err)
		assert.Empty(t, l.Phases())
	})

	refusals := []struct {
		name    string
		envID   func(*world) string
		wantErr error
	}{
		{"missing environment", func(*world) string { return "env_01JZ0000000000000000000099" }, delivery.ErrNotFound},
		{"archived environment", func(w *world) string { return w.env(t, wsActive, "old", true).ID() }, delivery.ErrArchived},
		{"environment in another workspace", func(w *world) string { return w.env(t, wsOther, "staging", false).ID() }, delivery.ErrWorkspaceMismatch},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			good := w.env(t, wsActive, "good", false)
			envID := tt.envID(w)
			before := w.state.clone()

			_, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{
				OrganizationID: org, WorkspaceID: wsActive, DisplayName: "L",
				Phases: []delivery.LifecyclePhaseProps{{Name: "one", EnvironmentIDs: []string{good.ID()}}, {Name: "two", EnvironmentIDs: []string{envID}}},
			})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, w.state)
		})
	}

	for name, tc := range map[string]struct {
		workspaceID string
		want        error
	}{
		"archived workspace": {wsArchived, delivery.ErrArchived},
		"missing workspace":  {"wsp_01JZ0000000000000000000099", delivery.ErrNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: tc.workspaceID, DisplayName: "L"})
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, w.lifecycles)
			assert.Empty(t, w.audit)
			assert.Empty(t, w.events)
		})
	}

	t.Run("slug taken", func(t *testing.T) {
		w := newWorld(t)
		w.lifecycle(t, wsActive, "mainline", false)
		_, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: wsActive, Slug: "mainline", DisplayName: "L"})
		require.ErrorIs(t, err, delivery.ErrSlugTaken)
		assert.Empty(t, w.audit)
	})

	t.Run("an environment in two phases", func(t *testing.T) {
		w := newWorld(t)
		e := w.env(t, wsActive, "staging", false)
		_, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{
			OrganizationID: org, WorkspaceID: wsActive, DisplayName: "L",
			Phases: []delivery.LifecyclePhaseProps{{Name: "a", EnvironmentIDs: []string{e.ID()}}, {Name: "b", EnvironmentIDs: []string{e.ID()}}},
		})
		require.ErrorIs(t, err, delivery.ErrInvalid)
	})
}

// Every operation asks the authorizer the right question, about the
// Lifecycle's Workspace, before it does anything.
func TestOperationsAreAuthorizedOnTheWorkspace(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	env := w.env(t, wsActive, "staging", false)
	spare := w.env(t, wsActive, "qa", false)
	l := w.lifecycle(t, wsActive, "mainline", false, env)
	phase := l.Phases()[0].ID
	create := delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: wsActive, Slug: "hotfix", DisplayName: "Hotfix"}
	addQA := delivery.LifecyclePhaseProps{Name: "qa", EnvironmentIDs: []string{spare.ID()}}
	rename := delivery.LifecyclePhaseProps{Name: "renamed", EnvironmentIDs: []string{env.ID()}}

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
		proposal   any
	}{
		{"create", func() error {
			_, err := w.svc.CreateLifecycle(ctx, ada, create)
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleCreate, create},
		{"get", func() error {
			_, err := w.svc.GetLifecycle(ctx, ada, org, l.ID())
			return err
		}, delivery.PermissionLifecycleRead, delivery.ActionLifecycleRead, nil},
		{"list", func() error {
			_, err := w.svc.ListLifecycles(ctx, ada, delivery.ListLifecyclesQuery{OrganizationID: org, WorkspaceID: wsActive})
			return err
		}, delivery.PermissionLifecycleRead, delivery.ActionLifecycleList, nil},
		{"update", func() error {
			_, err := w.svc.UpdateLifecycle(ctx, ada, org, l.ID(), delivery.UpdateLifecycleProps{DisplayName: ptr("Main")})
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleUpdate, delivery.UpdateLifecycleProps{DisplayName: ptr("Main")}},
		{"add phase", func() error {
			_, err := w.svc.AddLifecyclePhase(ctx, ada, org, l.ID(), addQA)
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleAddPhase, addQA},
		{"update phase", func() error {
			_, err := w.svc.UpdateLifecyclePhase(ctx, ada, org, l.ID(), phase, rename)
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleUpdatePhase, rename},
		{"move phase", func() error {
			_, err := w.svc.MoveLifecyclePhase(ctx, ada, org, l.ID(), phase, 1)
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleMovePhase, 1},
		{"remove phase", func() error {
			_, err := w.svc.RemoveLifecyclePhase(ctx, ada, org, l.ID(), phase)
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleRemovePhase, phase},
		{"archive", func() error {
			_, err := w.svc.ArchiveLifecycle(ctx, ada, org, l.ID())
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleArchive, nil},
		{"restore", func() error {
			_, err := w.svc.RestoreLifecycle(ctx, ada, org, l.ID())
			return err
		}, delivery.PermissionLifecycleManage, delivery.ActionLifecycleRestore, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.reset()
			require.NoError(t, tc.call())
			require.Len(t, w.authz.asked, 1)
			req := w.authz.asked[0]
			assert.Equal(t, ada, req.Actor)
			assert.Equal(t, org, req.OrganizationID)
			assert.Equal(t, tc.permission, req.Permission)
			assert.Equal(t, tc.action, req.Action)
			assert.Equal(t, wsActive, req.ResourceID)
			assert.Equal(t, tc.proposal, req.Proposal)
		})
	}
}

// A refused change writes nothing, whether it is denied or needs approval.
func TestRefusedChangesWriteNothing(t *testing.T) {
	ctx := context.Background()
	type fixture struct {
		env, spare *delivery.Environment
		l, old     *delivery.Lifecycle
	}
	cases := map[string]func(w *world, f fixture) error{
		"create": func(w *world, f fixture) error {
			_, err := w.svc.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Hotfix"})
			return err
		},
		"update": func(w *world, f fixture) error {
			_, err := w.svc.UpdateLifecycle(ctx, ada, org, f.l.ID(), delivery.UpdateLifecycleProps{DisplayName: ptr("Renamed")})
			return err
		},
		"add phase": func(w *world, f fixture) error {
			_, err := w.svc.AddLifecyclePhase(ctx, ada, org, f.l.ID(), delivery.LifecyclePhaseProps{Name: "qa", EnvironmentIDs: []string{f.spare.ID()}})
			return err
		},
		"update phase": func(w *world, f fixture) error {
			_, err := w.svc.UpdateLifecyclePhase(ctx, ada, org, f.l.ID(), f.l.Phases()[0].ID, delivery.LifecyclePhaseProps{Name: "x", EnvironmentIDs: []string{f.env.ID()}})
			return err
		},
		"move phase": func(w *world, f fixture) error {
			_, err := w.svc.MoveLifecyclePhase(ctx, ada, org, f.l.ID(), f.l.Phases()[0].ID, 0)
			return err
		},
		"remove phase": func(w *world, f fixture) error {
			_, err := w.svc.RemoveLifecyclePhase(ctx, ada, org, f.l.ID(), f.l.Phases()[0].ID)
			return err
		},
		"archive": func(w *world, f fixture) error {
			_, err := w.svc.ArchiveLifecycle(ctx, ada, org, f.l.ID())
			return err
		},
		"restore": func(w *world, f fixture) error {
			_, err := w.svc.RestoreLifecycle(ctx, ada, org, f.old.ID())
			return err
		},
	}
	outcomes := map[authorization.Outcome]error{
		authorization.OutcomeDeny:             authorization.ErrDenied,
		authorization.OutcomeRequiresApproval: authorization.ErrApprovalRequired,
	}
	for outcome, wantErr := range outcomes {
		for name, call := range cases {
			t.Run(string(outcome)+"/"+name, func(t *testing.T) {
				w := newWorld(t)
				var f fixture
				f.env = w.env(t, wsActive, "staging", false)
				f.spare = w.env(t, wsActive, "qa", false)
				f.l = w.lifecycle(t, wsActive, "mainline", false, f.env)
				f.old = w.lifecycle(t, wsActive, "old", true)
				w.authz.outcome = outcome
				before := w.state.clone()

				err := call(w, f)

				require.ErrorIs(t, err, wantErr)
				assert.Equal(t, before, w.state)
			})
		}
	}
}

// The service under the wrapper refuses to change anything without a
// decision, so wiring it up without authorization fails loudly.
func TestServiceWithoutAuthorizationChangesNothing(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	env := w.env(t, wsActive, "staging", false)
	l := w.lifecycle(t, wsActive, "mainline", false, env)
	old := w.lifecycle(t, wsActive, "old", true)
	core := w.svc.(*authorized).next
	before := w.state.clone()

	_, err := core.CreateLifecycle(ctx, ada, delivery.CreateLifecycleInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Hotfix"})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateLifecycle(ctx, ada, org, l.ID(), delivery.UpdateLifecycleProps{DisplayName: ptr("Renamed")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.RemoveLifecyclePhase(ctx, ada, org, l.ID(), l.Phases()[0].ID)
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveLifecycle(ctx, ada, org, l.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.RestoreLifecycle(ctx, ada, org, old.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)

	assert.Equal(t, before, w.state)
}

func TestReadLifecycles(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	l := w.lifecycle(t, wsActive, "mainline", false)
	w.lifecycle(t, wsActive, "hotfix", false)

	got, err := w.svc.GetLifecycle(ctx, ada, org, l.ID())
	require.NoError(t, err)
	assert.Equal(t, l.Snapshot(), got.Snapshot())

	_, err = w.svc.GetLifecycle(ctx, ada, org, "lfc_01JZ0000000000000000000099")
	require.ErrorIs(t, err, delivery.ErrNotFound)

	page, err := w.svc.ListLifecycles(ctx, ada, delivery.ListLifecyclesQuery{OrganizationID: org, WorkspaceID: wsActive})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)

	w.authz.deny = func(authorization.Request) bool { return true }
	got, err = w.svc.GetLifecycle(ctx, ada, org, l.ID())
	require.ErrorIs(t, err, authorization.ErrDenied)
	assert.Nil(t, got)
}

func TestLifecyclePhases(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	staging := w.env(t, wsActive, "staging", false)
	prod := w.env(t, wsActive, "prod", false)
	qa := w.env(t, wsActive, "qa", false)
	l := w.lifecycle(t, wsActive, "mainline", false, staging)
	stagingPhase := l.Phases()[0].ID

	t.Run("update the lifecycle", func(t *testing.T) {
		got, err := w.svc.UpdateLifecycle(ctx, ada, org, l.ID(), delivery.UpdateLifecycleProps{Description: ptr("Main path")})
		require.NoError(t, err)
		assert.Equal(t, "Main path", *got.Description())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventLifecycleUpdated, ev.Type)
		assert.Equal(t, "dec-"+wsActive, ev.DecisionID)
	})

	t.Run("add", func(t *testing.T) {
		got, err := w.svc.AddLifecyclePhase(ctx, ada, org, l.ID(), delivery.LifecyclePhaseProps{Name: "prod", EnvironmentIDs: []string{prod.ID()}})
		require.NoError(t, err)
		assert.Equal(t, []string{staging.ID(), prod.ID()}, got.EnvironmentIDs())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventLifecycleUpdated, ev.Type)
		assert.Len(t, ev.Before.(delivery.LifecycleSnapshot).Phases, 1)
		assert.Len(t, ev.After.(delivery.LifecycleSnapshot).Phases, 2)
	})

	t.Run("add checks the environments", func(t *testing.T) {
		before := w.state.clone()
		_, err := w.svc.AddLifecyclePhase(ctx, ada, org, l.ID(), delivery.LifecyclePhaseProps{Name: "nope", EnvironmentIDs: []string{"env_01JZ0000000000000000000099"}})
		require.ErrorIs(t, err, delivery.ErrNotFound)
		assert.Equal(t, before, w.state)
	})

	t.Run("update checks the environments", func(t *testing.T) {
		foreign := w.env(t, wsOther, "foreign", false)
		_, err := w.svc.UpdateLifecyclePhase(ctx, ada, org, l.ID(), stagingPhase, delivery.LifecyclePhaseProps{Name: "staging", EnvironmentIDs: []string{foreign.ID()}})
		require.ErrorIs(t, err, delivery.ErrWorkspaceMismatch)
	})

	t.Run("update", func(t *testing.T) {
		got, err := w.svc.UpdateLifecyclePhase(ctx, ada, org, l.ID(), stagingPhase, delivery.LifecyclePhaseProps{Name: "pre-prod", EnvironmentIDs: []string{staging.ID(), qa.ID()}, MinEnvironments: ptr(1)})
		require.NoError(t, err)
		p := got.Phases()[0]
		assert.Equal(t, stagingPhase, p.ID, "the phase keeps its ID")
		assert.Equal(t, "pre-prod", p.Name)
		assert.ElementsMatch(t, []string{staging.ID(), qa.ID()}, p.EnvironmentIDs)
	})

	t.Run("move", func(t *testing.T) {
		got, err := w.svc.MoveLifecyclePhase(ctx, ada, org, l.ID(), stagingPhase, 1)
		require.NoError(t, err)
		assert.Equal(t, stagingPhase, got.Phases()[1].ID)
		assert.Equal(t, got.Snapshot(), w.lifecycles[l.ID()])

		_, err = w.svc.MoveLifecyclePhase(ctx, ada, org, l.ID(), stagingPhase, 5)
		require.ErrorIs(t, err, delivery.ErrInvalid)
	})

	t.Run("an unknown phase", func(t *testing.T) {
		_, err := w.svc.RemoveLifecyclePhase(ctx, ada, org, l.ID(), "lfp_01JZ0000000000000000000099")
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("remove", func(t *testing.T) {
		got, err := w.svc.RemoveLifecyclePhase(ctx, ada, org, l.ID(), stagingPhase)
		require.NoError(t, err)
		assert.Equal(t, []string{prod.ID()}, got.EnvironmentIDs())

		n, err := w.CountActiveLifecyclesWithEnvironment(ctx, org, staging.ID())
		require.NoError(t, err)
		assert.Zero(t, n, "an environment removed from every lifecycle can be archived")
	})

	t.Run("every change was recorded in the outbox", func(t *testing.T) {
		assert.Equal(t, []string{
			"delivery.v1.lifecycle.updated", "delivery.v1.lifecycle.phase_added", "delivery.v1.lifecycle.phase_updated",
			"delivery.v1.lifecycle.phase_moved", "delivery.v1.lifecycle.phase_removed",
		}, w.eventTypes())
		for _, e := range w.events {
			assert.Equal(t, l.ID(), e.AggregateID)
		}
	})
}

func TestArchiveAndRestoreLifecycle(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	e := w.env(t, wsActive, "staging", false)
	l := w.lifecycle(t, wsActive, "mainline", false, e)

	t.Run("refused while an active channel follows it", func(t *testing.T) {
		w.channelUse[l.ID()] = 2
		t.Cleanup(func() { delete(w.channelUse, l.ID()) })
		before := w.state.clone()
		_, err := w.svc.ArchiveLifecycle(ctx, ada, org, l.ID())
		require.ErrorIs(t, err, delivery.ErrInUse)
		assert.Equal(t, before, w.state)
	})

	t.Run("archived once no active channel follows it", func(t *testing.T) {
		got, err := w.svc.ArchiveLifecycle(ctx, ada, org, l.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, delivery.EventLifecycleArchived, w.lastAudit().Type)

		w.reset()
		_, err = w.svc.ArchiveLifecycle(ctx, ada, org, l.ID())
		require.NoError(t, err)
		assert.Empty(t, w.audit, "archiving an archived lifecycle records nothing")
	})

	t.Run("an archived lifecycle cannot change", func(t *testing.T) {
		_, err := w.svc.AddLifecyclePhase(ctx, ada, org, l.ID(), delivery.LifecyclePhaseProps{Name: "x", EnvironmentIDs: []string{e.ID()}})
		require.ErrorIs(t, err, delivery.ErrArchived)
	})

	t.Run("an archived lifecycle frees its environments", func(t *testing.T) {
		n, err := w.CountActiveLifecyclesWithEnvironment(ctx, org, e.ID())
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("restore is refused while an environment in it is archived", func(t *testing.T) {
		e.Archive()
		w.setEnv(e)
		before := w.state.clone()
		_, err := w.svc.RestoreLifecycle(ctx, ada, org, l.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, w.state)
	})

	t.Run("restored once its environments are active", func(t *testing.T) {
		e.Restore()
		w.setEnv(e)

		got, err := w.svc.RestoreLifecycle(ctx, ada, org, l.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventLifecycleRestored, ev.Type)
		assert.NotNil(t, ev.Before.(delivery.LifecycleSnapshot).DeletedAt)
	})

	t.Run("restore is refused in an archived workspace", func(t *testing.T) {
		empty := w.lifecycle(t, wsActive, "empty", true)
		require.NoError(t, w.workspaces[wsActive].Archive())
		before := w.state.clone()

		_, err := w.svc.RestoreLifecycle(ctx, ada, org, empty.ID())

		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, w.state)
	})
}
