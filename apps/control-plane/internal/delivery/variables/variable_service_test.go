package variables

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var (
	ctx   = context.Background()
	actor = identity.Actor{PrincipalID: "prn_ada"}
)

func envScope(ids ...string) delivery.VariableScope {
	return delivery.VariableScope{EnvironmentIDs: ids}
}

func (w *world) create(t *testing.T, owner delivery.VariableOwner, name, path string, values ...delivery.VariableValueProps) (*delivery.Variable, error) {
	t.Helper()
	return w.svc.CreateVariable(ctx, actor, delivery.CreateVariableInput{
		OrganizationID: org, Owner: owner, Name: name, Kind: delivery.VariableKindText, ValuesPath: path, Values: values,
	})
}

func (w *world) inProject() delivery.VariableOwner {
	return delivery.VariableOwner{ProjectID: w.project}
}
func (w *world) inBilling() delivery.VariableOwner {
	return delivery.VariableOwner{ApplicationID: w.billing.ID()}
}

func TestCreateVariable(t *testing.T) {
	w := newWorld(t)
	// The Application is named by its slug.
	v, err := w.create(t, delivery.VariableOwner{ApplicationID: "billing"}, "Log.Level", "log.level",
		delivery.VariableValueProps{Value: "info"},
		delivery.VariableValueProps{Value: "warn", Scope: envScope(w.prod.ID())})
	require.NoError(t, err)

	assert.Equal(t, w.billing.ID(), *v.ApplicationID())
	assert.Equal(t, []string{"delivery.variables:" + w.project}, w.locked, "the project's variables are held for the checks")
	assert.Equal(t, []string{w.project, w.prod.ID()}, w.authz.askedOn(delivery.PermissionVariableManage),
		"on the application's project, and on the environment a value is scoped to")
	require.Len(t, w.audit, 1)
	assert.Equal(t, delivery.EventVariableCreated, w.audit[0].Type)
	assert.Equal(t, "dec-"+w.project, w.audit[0].DecisionID)
	require.Len(t, w.events, 1)
	assert.Equal(t, "delivery.v1.variable.created", w.events[0].Type)
}

// A Project's members cannot change an Environment's configuration without
// being allowed to on that Environment.
func TestScopedValuesNeedPermissionOnTheirEnvironments(t *testing.T) {
	w := newWorld(t)
	v, err := w.create(t, w.inProject(), "Database.Host", "",
		delivery.VariableValueProps{Value: "db.internal"},
		delivery.VariableValueProps{Value: "db.staging", Scope: envScope(w.staging.ID())})
	require.NoError(t, err)
	unscoped, staged := v.Values()[0].ID, v.Values()[1].ID
	w.authz.deny = func(req authorization.Request) bool { return req.ResourceID == w.prod.ID() }

	toTarget := delivery.VariableScope{TargetIDs: []string{w.prodEU.ID()}}
	tests := []struct {
		name    string
		change  func() error
		allowed bool
	}{
		{"change the unscoped value", func() error {
			_, err := w.svc.UpdateVariableValue(ctx, actor, org, v.ID(), unscoped, delivery.VariableValueProps{Value: "db.local"})
			return err
		}, true},
		{"add a value for production", func() error {
			_, err := w.svc.AddVariableValue(ctx, actor, org, v.ID(), delivery.VariableValueProps{Value: "db.prod", Scope: envScope(w.prod.ID())})
			return err
		}, false},
		{"add a value for a target in production", func() error {
			_, err := w.svc.AddVariableValue(ctx, actor, org, v.ID(), delivery.VariableValueProps{Value: "db.eu", Scope: toTarget})
			return err
		}, false},
		{"move the staging value to production", func() error {
			_, err := w.svc.UpdateVariableValue(ctx, actor, org, v.ID(), staged, delivery.VariableValueProps{Value: "db.staging", Scope: envScope(w.prod.ID())})
			return err
		}, false},
		{"create a variable with a production value", func() error {
			_, err := w.create(t, w.inProject(), "Api.Url", "", delivery.VariableValueProps{Value: "x", Scope: envScope(w.prod.ID())})
			return err
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.change()
			if tt.allowed {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, authorization.ErrDenied)
		})
	}

	t.Run("changes to every value need every environment", func(t *testing.T) {
		w.authz.deny = nil
		_, err := w.svc.AddVariableValue(ctx, actor, org, v.ID(), delivery.VariableValueProps{Value: "db.prod", Scope: envScope(w.prod.ID())})
		require.NoError(t, err)
		w.authz.deny = func(req authorization.Request) bool { return req.ResourceID == w.prod.ID() }

		_, err = w.svc.UpdateVariable(ctx, actor, org, v.ID(), delivery.UpdateVariableProps{ValuesPath: new("database.host")})
		require.ErrorIs(t, err, authorization.ErrDenied)
		_, err = w.svc.ArchiveVariable(ctx, actor, org, v.ID())
		require.ErrorIs(t, err, authorization.ErrDenied)
	})
}

func TestScopesMustBeTheVariablesToUse(t *testing.T) {
	w := newWorld(t)
	archived := w.staging
	tests := []struct {
		name  string
		owner delivery.VariableOwner
		scope delivery.VariableScope
		is    error
	}{
		{"an environment in another workspace", w.inProject(), envScope(w.far.ID()), delivery.ErrWorkspaceMismatch},
		{"another application's channel", w.inBilling(), delivery.VariableScope{ChannelIDs: []string{w.otherApps.ID()}}, delivery.ErrInvalid},
		{"an archived environment", w.inProject(), envScope(archived.ID()), delivery.ErrArchived},
	}
	archived.Archive()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.create(t, tt.owner, "Feature.Flag", "", delivery.VariableValueProps{Value: "on", Scope: tt.scope})
			require.ErrorIs(t, err, tt.is)
			assert.Empty(t, w.variables)
		})
	}

	_, err := w.create(t, w.inBilling(), "Feature.Flag", "", delivery.VariableValueProps{Value: "on", Scope: delivery.VariableScope{ChannelIDs: []string{w.hotfix.ID()}}})
	assert.NoError(t, err, "its own application's channel")
}

// A save reads what its scope names, and its Application, under the row locks
// those records' own changes take: whichever runs first, the other sees it, so
// an active value never names an archived record.
func TestAScopeIsReadLocked(t *testing.T) {
	t.Run("a save reads the application, its targets, and their environments locked", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.create(t, w.inBilling(), "Feature.Flag", "", delivery.VariableValueProps{
			Value: "on", Scope: delivery.VariableScope{TargetIDs: []string{w.prodEU.ID()}},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, w.lockedApplications, "the variable's application is held")
		assert.Equal(t, 1, w.lockedTargets, "the target is read for update")
		assert.Equal(t, 1, w.lockedEnvironments, "so is its environment")
	})

	t.Run("a target archived just before the save is refused", func(t *testing.T) {
		w := newWorld(t)
		w.onLock = func() { w.targets[w.prodEU.ID()].Archive() }

		_, err := w.create(t, w.inProject(), "Feature.Flag", "", delivery.VariableValueProps{
			Value: "on", Scope: delivery.VariableScope{TargetIDs: []string{w.prodEU.ID()}},
		})
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Empty(t, w.variables, "nothing is saved")
	})

	t.Run("an environment archived just before the save is refused", func(t *testing.T) {
		w := newWorld(t)
		w.onLock = func() { w.environments[w.prod.ID()].Archive() }

		_, err := w.create(t, w.inProject(), "Feature.Flag", "", delivery.VariableValueProps{
			Value: "on", Scope: envScope(w.prod.ID()),
		})
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Empty(t, w.variables)
	})

	t.Run("an application archived just before the save is refused", func(t *testing.T) {
		w := newWorld(t)
		w.onLock = func() { w.applications[w.billing.ID()].Archive() }

		_, err := w.create(t, w.inBilling(), "Feature.Flag", "", delivery.VariableValueProps{Value: "on"})
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Empty(t, w.variables)
	})
}

// The rules that span variables are checked for every Application in the
// Project, whichever side the change is on.
func TestVariablesAnApplicationDeploysWithMustAgree(t *testing.T) {
	t.Run("an application's variable shadows its project's only in value", func(t *testing.T) {
		w := newWorld(t)
		shared, err := w.create(t, w.inProject(), "Database.Host", "database.host")
		require.NoError(t, err)

		_, err = w.create(t, w.inBilling(), "Database.Host", "db.host")
		require.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Contains(t, err.Error(), "shadows")

		_, err = w.create(t, w.inBilling(), "Database.Host", "database.host", delivery.VariableValueProps{Value: "billing-db"})
		require.NoError(t, err)

		_, err = w.svc.UpdateVariable(ctx, actor, org, shared.ID(), delivery.UpdateVariableProps{ValuesPath: new("db.host")})
		require.ErrorIs(t, err, delivery.ErrInvalid, "the project's variable cannot move while an application shadows it")
	})

	t.Run("no two write overlapping values paths", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.create(t, w.inBilling(), "Database.Host", "database.host")
		require.NoError(t, err)

		_, err = w.create(t, w.inProject(), "Database", "database")
		require.ErrorIs(t, err, delivery.ErrInvalid, "it would overwrite the application's variable")
		assert.Contains(t, err.Error(), "overlapping")
		assert.Len(t, w.variables, 1)

		other, err := w.create(t, w.inProject(), "Cache.Host", "cache.host")
		require.NoError(t, err)
		_, err = w.svc.UpdateVariable(ctx, actor, org, other.ID(), delivery.UpdateVariableProps{ValuesPath: new("database.host.name")})
		require.ErrorIs(t, err, delivery.ErrInvalid)

		_, err = w.create(t, delivery.VariableOwner{ApplicationID: w.checkout.ID()}, "Db", "database")
		assert.NoError(t, err, "another application deploys with its own")
	})

	t.Run("an archived variable no longer counts", func(t *testing.T) {
		w := newWorld(t)
		old, err := w.create(t, w.inProject(), "Database.Host", "database.host")
		require.NoError(t, err)
		w.locked = nil
		_, err = w.svc.ArchiveVariable(ctx, actor, org, old.ID())
		require.NoError(t, err)
		assert.Equal(t, delivery.EventVariableArchived, w.audit[len(w.audit)-1].Type)
		assert.Equal(t, []string{"delivery.variables:" + w.project}, w.locked, "an archive takes its turn with the other changes")

		_, err = w.create(t, w.inBilling(), "Database", "database")
		assert.NoError(t, err)
		_, err = w.svc.AddVariableValue(ctx, actor, org, old.ID(), delivery.VariableValueProps{Value: "x"})
		assert.ErrorIs(t, err, delivery.ErrArchived)
	})
}

// The Environments a change needs permission on are read before the
// Project's variables are held. A value moved into another Environment in
// between must not be removed on a decision that never covered it.
func TestAVariableChangedSinceItWasAuthorizedIsRefused(t *testing.T) {
	w := newWorld(t)
	v, err := w.create(t, w.inProject(), "Database.Host", "database.host",
		delivery.VariableValueProps{Value: "db.staging", Scope: envScope(w.staging.ID())})
	require.NoError(t, err)
	valueID := v.Snapshot().Values[0].ID
	w.onLock = func() {
		snap := w.variables[v.ID()]
		snap.Values = slices.Clone(snap.Values)
		snap.Values[0].Scope = envScope(w.prod.ID())
		snap.UpdatedAt = snap.UpdatedAt.Add(time.Second)
		w.variables[v.ID()] = snap
	}

	_, err = w.svc.RemoveVariableValue(ctx, actor, org, v.ID(), valueID)
	require.ErrorIs(t, err, delivery.ErrConflict)
	_, err = w.svc.ArchiveVariable(ctx, actor, org, v.ID())
	require.ErrorIs(t, err, delivery.ErrConflict)

	w.onLock = nil
	_, err = w.svc.RemoveVariableValue(ctx, actor, org, v.ID(), valueID)
	require.NoError(t, err, "read again, the change is authorized on what is stored")
}

func TestAnArchivedApplicationsVariablesDoNotChange(t *testing.T) {
	w := newWorld(t)
	v, err := w.create(t, w.inBilling(), "Log.Level", "")
	require.NoError(t, err)
	w.billing.Archive()

	_, err = w.svc.AddVariableValue(ctx, actor, org, v.ID(), delivery.VariableValueProps{Value: "info"})
	require.ErrorIs(t, err, delivery.ErrArchived)
	_, err = w.create(t, w.inBilling(), "Other", "")
	require.ErrorIs(t, err, delivery.ErrArchived)
}

func TestListVariables(t *testing.T) {
	w := newWorld(t)
	for _, v := range []struct {
		owner delivery.VariableOwner
		name  string
	}{{w.inProject(), "Region"}, {w.inProject(), "Api.Url"}, {w.inBilling(), "Log.Level"}} {
		_, err := w.create(t, v.owner, v.name, "")
		require.NoError(t, err)
	}
	names := func(query delivery.ListVariablesQuery) []string {
		query.OrganizationID = org
		vs, err := w.svc.ListVariables(ctx, actor, query)
		require.NoError(t, err)
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = v.Name()
		}
		return out
	}
	w.authz.asked = nil

	assert.Equal(t, []string{"Api.Url", "Region"}, names(delivery.ListVariablesQuery{Owner: w.inProject()}))
	assert.Equal(t, []string{"Log.Level"}, names(delivery.ListVariablesQuery{Owner: w.inBilling()}))
	assert.Equal(t, []string{"Log.Level", "Api.Url", "Region"},
		names(delivery.ListVariablesQuery{Owner: delivery.VariableOwner{ApplicationID: "billing"}, IncludeInherited: true}),
		"an application's own, then its project's")
	assert.Equal(t, []string{w.project, w.project, w.project}, w.authz.askedOn(delivery.PermissionVariableRead))
}

// A secret value may use only an active Secret Store in the variable's
// Workspace that the Environments it names may read.
func TestSecretValuesAreCheckedAgainstTheirStore(t *testing.T) {
	const here, stagingOnly, elsewhere, archived = "sst_01BX5ZZKBKACTAV9WEVGEMMVR1", "sst_01BX5ZZKBKACTAV9WEVGEMMVR2", "sst_01BX5ZZKBKACTAV9WEVGEMMVR3", "sst_01BX5ZZKBKACTAV9WEVGEMMVR4"
	w := newWorld(t)
	ws := w.projects[w.project].WorkspaceID()
	w.stores = map[string]delivery.SecretStoreRef{
		here:        {ID: here, WorkspaceID: ws, DisplayName: "In-cluster"},
		stagingOnly: {ID: stagingOnly, WorkspaceID: ws, DisplayName: "Staging vault", EnvironmentIDs: []string{w.staging.ID()}},
		elsewhere:   {ID: elsewhere, WorkspaceID: w.far.WorkspaceID(), DisplayName: "Elsewhere"},
		archived:    {ID: archived, WorkspaceID: ws, DisplayName: "Old", Archived: true},
	}
	secret := func(store string, scope delivery.VariableScope) error {
		_, err := w.svc.CreateVariable(ctx, actor, delivery.CreateVariableInput{
			OrganizationID: org, Owner: w.inBilling(), Name: "Database.Password", Kind: delivery.VariableKindSecret,
			Values: []delivery.VariableValueProps{{Secret: &delivery.SecretReference{StoreID: store, Path: "db"}, Scope: scope}},
		})
		w.variables = map[string]delivery.VariableSnapshot{}
		return err
	}

	require.NoError(t, secret(here, envScope(w.prod.ID())))
	assert.Equal(t, 1, w.lockedStores, "the store is held, so it cannot be archived while the value is saved")
	require.NoError(t, secret(stagingOnly, envScope(w.staging.ID())))
	require.NoError(t, secret(stagingOnly, delivery.VariableScope{}), "a value naming no environment is checked when it is deployed")

	tests := []struct {
		name  string
		store string
		scope delivery.VariableScope
		is    error
	}{
		{"a store the environment may not read", stagingOnly, envScope(w.prod.ID()), delivery.ErrInvalid},
		{"through a target in that environment", stagingOnly, delivery.VariableScope{TargetIDs: []string{w.prodEU.ID()}}, delivery.ErrInvalid},
		{"a store in another workspace", elsewhere, delivery.VariableScope{}, delivery.ErrWorkspaceMismatch},
		{"an archived store", archived, delivery.VariableScope{}, delivery.ErrArchived},
		{"a store that does not exist", "sst_01BX5ZZKBKACTAV9WEVGEMMVR9", delivery.VariableScope{}, delivery.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, secret(tt.store, tt.scope), tt.is)
		})
	}
}
