package environments

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

func ptr[T any](v T) *T { return &v }

func TestNewEnvironmentServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]EnvironmentServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewEnvironmentService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewEnvironmentService(all...)
	assert.NoError(t, err)
}

func TestCreateEnvironment(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	e, err := w.svc.CreateEnvironment(ctx, ada, delivery.CreateEnvironmentInput{
		OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Production", Tier: delivery.TierProduction,
	})
	require.NoError(t, err)

	assert.Equal(t, "production", e.Slug(), "slug derived from the display name")
	require.Len(t, w.authz.asked, 1)
	req := w.authz.asked[0]
	assert.Equal(t, delivery.PermissionEnvironmentManage, req.Permission)
	assert.Equal(t, delivery.ActionEnvironmentCreate, req.Action)
	assert.Equal(t, wsActive, req.ResourceID, "authorized on the Workspace")

	assert.Contains(t, w.environments, e.ID())
	assert.Equal(t, iam.Resource{ID: e.ID(), OrganizationID: org, Type: iam.ResourceEnvironment, ParentID: wsActive}, w.resources[e.ID()])
	assert.Equal(t, []string{"delivery.v1.environment.created"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, delivery.EventEnvironmentCreated, w.audit[0].Type)
	assert.Equal(t, "dec-"+wsActive, w.audit[0].DecisionID)
	assert.Equal(t, ada, w.audit[0].Actor)
	assert.Equal(t, e.Snapshot(), w.audit[0].After)
}

func TestCreateEnvironmentNeedsAnActiveWorkspace(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		workspaceID string
		want        error
	}{
		"archived": {wsArchived, delivery.ErrArchived},
		"missing":  {"wsp_01JZ0000000000000000000099", delivery.ErrNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.svc.CreateEnvironment(ctx, ada, delivery.CreateEnvironmentInput{
				OrganizationID: org, WorkspaceID: tc.workspaceID, DisplayName: "Production", Tier: delivery.TierProduction,
			})
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, w.environments)
			assert.Empty(t, w.resources)
			assert.Empty(t, w.audit)
			assert.Empty(t, w.events)
		})
	}
}

// Every operation asks the authorizer the right question, about the right
// resource, before it does anything.
func TestOperationsAreAuthorizedOnTheirResource(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	env := w.env(t, wsActive, "staging", false)
	archivedEnv := w.env(t, wsActive, "old", true)
	tg := w.target(t, env.ID(), "web", false)
	archivedTg := w.target(t, env.ID(), "api", true)

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
		resource   string
	}{
		{"create environment", func() error {
			_, err := w.svc.CreateEnvironment(ctx, ada, delivery.CreateEnvironmentInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Production", Tier: delivery.TierProduction})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionEnvironmentCreate, wsActive},
		{"get environment", func() error {
			_, err := w.svc.GetEnvironment(ctx, ada, org, env.ID())
			return err
		}, delivery.PermissionEnvironmentRead, delivery.ActionEnvironmentRead, env.ID()},
		{"get environment by slug", func() error {
			_, err := w.svc.GetEnvironmentBySlug(ctx, ada, org, wsActive, "staging")
			return err
		}, delivery.PermissionEnvironmentRead, delivery.ActionEnvironmentRead, env.ID()},
		{"list environments", func() error {
			_, err := w.svc.ListEnvironments(ctx, ada, delivery.ListEnvironmentsQuery{OrganizationID: org, WorkspaceID: wsActive})
			return err
		}, delivery.PermissionEnvironmentRead, delivery.ActionEnvironmentList, wsActive},
		{"update environment", func() error {
			_, err := w.svc.UpdateEnvironment(ctx, ada, org, env.ID(), delivery.UpdateEnvironmentProps{DisplayName: ptr("Staging 2")})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionEnvironmentUpdate, env.ID()},
		{"archive environment", func() error {
			_, err := w.svc.ArchiveEnvironments(ctx, ada, org, []string{archivedEnv.ID()})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionEnvironmentArchive, archivedEnv.ID()},
		{"restore environment", func() error {
			_, err := w.svc.RestoreEnvironments(ctx, ada, org, []string{archivedEnv.ID()})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionEnvironmentRestore, wsActive},
		{"create target", func() error {
			_, err := w.svc.CreateTarget(ctx, ada, delivery.CreateTargetInput{OrganizationID: org, EnvironmentID: env.ID(), DisplayName: "Workers", Runtime: delivery.RuntimeKubernetes, Locator: locator()})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionTargetCreate, env.ID()},
		{"get target", func() error {
			_, err := w.svc.GetTarget(ctx, ada, org, tg.ID())
			return err
		}, delivery.PermissionEnvironmentRead, delivery.ActionTargetRead, env.ID()},
		{"list targets", func() error {
			_, err := w.svc.ListTargets(ctx, ada, delivery.ListTargetsQuery{OrganizationID: org, EnvironmentID: env.ID()})
			return err
		}, delivery.PermissionEnvironmentRead, delivery.ActionTargetList, env.ID()},
		{"update target", func() error {
			_, err := w.svc.UpdateTarget(ctx, ada, org, tg.ID(), delivery.UpdateTargetProps{DisplayName: ptr("Web 2")})
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionTargetUpdate, env.ID()},
		{"archive target", func() error {
			_, err := w.svc.ArchiveTarget(ctx, ada, org, archivedTg.ID())
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionTargetArchive, env.ID()},
		{"restore target", func() error {
			_, err := w.svc.RestoreTarget(ctx, ada, org, tg.ID())
			return err
		}, delivery.PermissionEnvironmentManage, delivery.ActionTargetRestore, env.ID()},
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
			assert.Equal(t, tc.resource, req.ResourceID)
		})
	}
}

// A refused change writes nothing.
func TestRefusedChangesWriteNothing(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(w *world, env *delivery.Environment, tg *delivery.Target) error{
		"create environment": func(w *world, _ *delivery.Environment, _ *delivery.Target) error {
			_, err := w.svc.CreateEnvironment(ctx, ada, delivery.CreateEnvironmentInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Production", Tier: delivery.TierProduction})
			return err
		},
		"update environment": func(w *world, env *delivery.Environment, _ *delivery.Target) error {
			_, err := w.svc.UpdateEnvironment(ctx, ada, org, env.ID(), delivery.UpdateEnvironmentProps{DisplayName: ptr("Renamed")})
			return err
		},
		"archive environment": func(w *world, env *delivery.Environment, _ *delivery.Target) error {
			_, err := w.svc.ArchiveEnvironments(ctx, ada, org, []string{env.ID()})
			return err
		},
		"create target": func(w *world, env *delivery.Environment, _ *delivery.Target) error {
			_, err := w.svc.CreateTarget(ctx, ada, delivery.CreateTargetInput{OrganizationID: org, EnvironmentID: env.ID(), DisplayName: "Workers", Runtime: delivery.RuntimeKubernetes, Locator: locator()})
			return err
		},
		"update target": func(w *world, _ *delivery.Environment, tg *delivery.Target) error {
			_, err := w.svc.UpdateTarget(ctx, ada, org, tg.ID(), delivery.UpdateTargetProps{DisplayName: ptr("Renamed")})
			return err
		},
		"archive target": func(w *world, _ *delivery.Environment, tg *delivery.Target) error {
			_, err := w.svc.ArchiveTarget(ctx, ada, org, tg.ID())
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			env := w.env(t, wsActive, "staging", false)
			tg := w.target(t, env.ID(), "web", false)
			before := w.state.clone()
			w.authz.deny = func(authorization.Request) bool { return true }

			err := call(w, env, tg)

			require.ErrorIs(t, err, authorization.ErrDenied)
			assert.Equal(t, before, w.state)
		})
	}
}

// The service under the wrapper refuses to change anything without a
// decision, so wiring it up without authorization fails loudly.
func TestServiceWithoutAuthorizationChangesNothing(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	env := w.env(t, wsActive, "staging", false)
	unused := w.env(t, wsActive, "unused", false)
	tg := w.target(t, env.ID(), "web", false)
	core := w.svc.(*authorized).next
	before := w.state.clone()

	_, err := core.CreateEnvironment(ctx, ada, delivery.CreateEnvironmentInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Production", Tier: delivery.TierProduction})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateEnvironment(ctx, ada, org, env.ID(), delivery.UpdateEnvironmentProps{DisplayName: ptr("Renamed")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveEnvironments(ctx, ada, org, []string{unused.ID()})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveTarget(ctx, ada, org, tg.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)

	assert.Equal(t, before, w.state)
}

func TestUpdateEnvironment(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	env := w.env(t, wsActive, "staging", false)

	got, err := w.svc.UpdateEnvironment(ctx, ada, org, env.ID(), delivery.UpdateEnvironmentProps{DisplayName: ptr("Staging EU"), Tier: ptr(delivery.TierProduction)})
	require.NoError(t, err)

	assert.Equal(t, "Staging EU", got.DisplayName())
	assert.Equal(t, delivery.TierProduction, w.environments[env.ID()].Tier)
	assert.Equal(t, []string{"delivery.v1.environment.updated"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, env.Snapshot(), w.audit[0].Before)
	assert.Equal(t, got.Snapshot(), w.audit[0].After)
	assert.Equal(t, "dec-"+env.ID(), w.audit[0].DecisionID)
}

func TestArchiveEnvironments(t *testing.T) {
	ctx := context.Background()

	t.Run("archives each, with its own decision", func(t *testing.T) {
		w := newWorld(t)
		a := w.env(t, wsActive, "a", false)
		b := w.env(t, wsActive, "b", false)

		got, err := w.svc.ArchiveEnvironments(ctx, ada, org, []string{a.ID(), b.ID(), a.ID()})
		require.NoError(t, err)

		require.Len(t, got, 2, "repeated IDs count once")
		assert.Len(t, w.authz.asked, 2)
		for i, e := range []*delivery.Environment{a, b} {
			assert.NotNil(t, w.environments[e.ID()].DeletedAt)
			assert.True(t, w.archivedRes[e.ID()])
			assert.Equal(t, e.ID(), w.audit[i].ResourceID)
			assert.Equal(t, "dec-"+e.ID(), w.audit[i].DecisionID)
		}
	})

	t.Run("leaves archived ones as they are", func(t *testing.T) {
		w := newWorld(t)
		e := w.env(t, wsActive, "old", true)
		got, err := w.svc.ArchiveEnvironments(ctx, ada, org, []string{e.ID()})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Empty(t, w.audit)
		assert.Empty(t, w.events)
	})

	for name, setup := range map[string]func(w *world, e *delivery.Environment){
		"with an active target":     func(w *world, e *delivery.Environment) { w.target(t, e.ID(), "web", false) },
		"in an active lifecycle":    func(w *world, e *delivery.Environment) { w.lifecycleUse[e.ID()] = 1 },
		"unknown in the same batch": func(w *world, _ *delivery.Environment) {},
	} {
		t.Run("refuses all when one is "+name, func(t *testing.T) {
			w := newWorld(t)
			free := w.env(t, wsActive, "free", false)
			busy := w.env(t, wsActive, "busy", false)
			setup(w, busy)
			ids := []string{free.ID(), busy.ID()}
			want := delivery.ErrInUse
			if name == "unknown in the same batch" {
				ids = []string{free.ID(), "env_01JZ0000000000000000000099"}
				want = delivery.ErrNotFound
			}
			before := w.state.clone()

			_, err := w.svc.ArchiveEnvironments(ctx, ada, org, ids)

			require.ErrorIs(t, err, want)
			assert.Equal(t, before, w.state, "the first one is not archived either")
		})
	}

	t.Run("refuses all when one is denied", func(t *testing.T) {
		w := newWorld(t)
		a := w.env(t, wsActive, "a", false)
		b := w.env(t, wsActive, "b", false)
		w.authz.deny = func(r authorization.Request) bool { return r.ResourceID == b.ID() }
		before := w.state.clone()

		_, err := w.svc.ArchiveEnvironments(ctx, ada, org, []string{a.ID(), b.ID()})

		require.ErrorIs(t, err, authorization.ErrDenied)
		assert.Equal(t, before, w.state)
	})
}

func TestRestoreEnvironments(t *testing.T) {
	ctx := context.Background()

	t.Run("restores, authorized on the workspace", func(t *testing.T) {
		w := newWorld(t)
		e := w.env(t, wsActive, "old", true)
		w.archivedRes[e.ID()] = true

		got, err := w.svc.RestoreEnvironments(ctx, ada, org, []string{e.ID()})
		require.NoError(t, err)

		require.Len(t, got, 1)
		assert.False(t, got[0].Archived())
		assert.Nil(t, w.environments[e.ID()].DeletedAt)
		assert.False(t, w.archivedRes[e.ID()])
		assert.Equal(t, e.ID(), w.authz.asked[0].Proposal)
		require.Len(t, w.audit, 1)
		assert.Equal(t, delivery.EventEnvironmentRestored, w.audit[0].Type)
		assert.Equal(t, "dec-"+wsActive, w.audit[0].DecisionID)
		assert.Equal(t, []string{"delivery.v1.environment.restored"}, w.eventTypes())
	})

	t.Run("refuses one in an archived workspace", func(t *testing.T) {
		w := newWorld(t)
		e := w.env(t, wsArchived, "old", true)
		_, err := w.svc.RestoreEnvironments(ctx, ada, org, []string{e.ID()})
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.NotNil(t, w.environments[e.ID()].DeletedAt)
	})

	t.Run("refuses an unknown one before asking", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.svc.RestoreEnvironments(ctx, ada, org, []string{"env_01JZ0000000000000000000099"})
		require.ErrorIs(t, err, delivery.ErrNotFound)
		assert.Empty(t, w.authz.asked)
	})
}

func TestCreateTarget(t *testing.T) {
	ctx := context.Background()
	in := func(envID string) delivery.CreateTargetInput {
		return delivery.CreateTargetInput{OrganizationID: org, EnvironmentID: envID, DisplayName: "Workers", Runtime: delivery.RuntimeKubernetes, Locator: locator()}
	}

	t.Run("adds it with no agent capabilities", func(t *testing.T) {
		w := newWorld(t)
		env := w.env(t, wsActive, "staging", false)
		tg, err := w.svc.CreateTarget(ctx, ada, in(env.ID()))
		require.NoError(t, err)
		assert.Equal(t, testClusterID, tg.ClusterID())
		assert.False(t, tg.CanObserve())
		assert.False(t, tg.CanExecute())
		assert.Contains(t, w.targets, tg.ID())
		assert.Equal(t, []string{delivery.EventTargetCreated}, w.auditTypes())
		assert.Equal(t, "dec-"+env.ID(), w.audit[0].DecisionID)
	})

	t.Run("refuses an archived environment", func(t *testing.T) {
		w := newWorld(t)
		env := w.env(t, wsActive, "old", true)
		_, err := w.svc.CreateTarget(ctx, ada, in(env.ID()))
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Empty(t, w.targets)
	})
}

func TestTargetChanges(t *testing.T) {
	ctx := context.Background()

	t.Run("archive twice records once", func(t *testing.T) {
		w := newWorld(t)
		env := w.env(t, wsActive, "staging", false)
		tg := w.target(t, env.ID(), "web", false)
		for range 2 {
			got, err := w.svc.ArchiveTarget(ctx, ada, org, tg.ID())
			require.NoError(t, err)
			assert.True(t, got.Archived())
		}
		assert.Equal(t, []string{delivery.EventTargetArchived}, w.auditTypes())
	})

	t.Run("restore needs an active environment", func(t *testing.T) {
		w := newWorld(t)
		env := w.env(t, wsActive, "old", true)
		tg := w.target(t, env.ID(), "web", true)
		_, err := w.svc.RestoreTarget(ctx, ada, org, tg.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.NotNil(t, w.targets[tg.ID()].DeletedAt)
	})

	t.Run("restore", func(t *testing.T) {
		w := newWorld(t)
		env := w.env(t, wsActive, "staging", false)
		tg := w.target(t, env.ID(), "web", true)
		got, err := w.svc.RestoreTarget(ctx, ada, org, tg.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, []string{delivery.EventTargetRestored}, w.auditTypes())
	})
}
