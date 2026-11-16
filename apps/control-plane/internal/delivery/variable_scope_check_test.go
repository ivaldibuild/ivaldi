package delivery

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// scopeRecords fakes the lookups the scope checker reads through, recording
// the IDs each locking read asked for.
type scopeRecords struct {
	targets      map[string]*Target
	environments map[string]*Environment
	stores       map[string]SecretStoreRef
	// targetReads, environmentReads, and storeReads are the IDs of each
	// call, in the order asked.
	targetReads, environmentReads, storeReads [][]string
}

func (r *scopeRecords) GetTargetsForUpdate(_ context.Context, _ string, ids []string) ([]*Target, error) {
	r.targetReads = append(r.targetReads, ids)
	var out []*Target
	for _, id := range ids {
		if t, ok := r.targets[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *scopeRecords) GetEnvironmentsForUpdate(_ context.Context, _ string, ids []string) ([]*Environment, error) {
	r.environmentReads = append(r.environmentReads, ids)
	var out []*Environment
	for _, id := range ids {
		if e, ok := r.environments[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *scopeRecords) GetChannel(context.Context, string, string) (*Channel, error) {
	return nil, ErrNotFound
}

func (r *scopeRecords) LookupSecretStoresForShare(_ context.Context, _ string, ids []string) (map[string]SecretStoreRef, error) {
	r.storeReads = append(r.storeReads, ids)
	out := map[string]SecretStoreRef{}
	for _, id := range ids {
		if s, ok := r.stores[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

// Restoring an Application checks all its variables at once: each table is
// read once, in ID order, so two writers that take several of the same rows
// take them in the same order and cannot deadlock.
func TestVariableScopeCheckerLocksInOneOrder(t *testing.T) {
	const org, ws = "org_scope", "wsp_scope"
	ctx := context.Background()
	appID := id.New("app")

	env := func(slug string) *Environment {
		return must(NewEnvironment(NewEnvironmentProps{OrganizationID: org, WorkspaceID: ws, Slug: slug, DisplayName: slug, Tier: TierStaging}, nil))
	}
	first, second := env("first"), env("second")
	if second.ID() < first.ID() {
		first, second = second, first
	}
	target := must(NewTarget(NewTargetProps{
		OrganizationID: org, EnvironmentID: first.ID(), ClusterID: id.New("cls"), Slug: "eu", DisplayName: "EU",
		Runtime: RuntimeKubernetes, Locator: map[string]string{LocatorCluster: "eu", LocatorNamespace: "shop"},
	}, nil))
	storeA, storeB := "sst_01BX5ZZKBKACTAV9WEVGEMMVR1", "sst_01BX5ZZKBKACTAV9WEVGEMMVR2"

	variable := func(name string, values ...NewVariableValueProps) *Variable {
		kind := VariableKindText
		if values[0].Secret != nil {
			kind = VariableKindSecret
		}
		return must(NewVariable(NewVariableProps{OrganizationID: org, ApplicationID: &appID, Name: name, Kind: kind, Values: values}, nil))
	}
	// The second Environment is named first, and the stores in reverse.
	variables := []*Variable{
		variable("Region", NewVariableValueProps{Value: "eu", Scope: VariableScope{EnvironmentIDs: []string{second.ID()}}}),
		variable("Cluster", NewVariableValueProps{Value: "eu", Scope: VariableScope{TargetIDs: []string{target.ID()}}}),
		variable("Api.Key", NewVariableValueProps{Secret: &SecretReference{StoreID: storeB, Path: "api"}}),
		variable("Db.Password", NewVariableValueProps{Secret: &SecretReference{StoreID: storeA, Path: "db"}}),
	}

	records := func() *scopeRecords {
		return &scopeRecords{
			targets:      map[string]*Target{target.ID(): target},
			environments: map[string]*Environment{first.ID(): first, second.ID(): second},
			stores: map[string]SecretStoreRef{
				storeA: {ID: storeA, WorkspaceID: ws, DisplayName: "A"},
				storeB: {ID: storeB, WorkspaceID: ws, DisplayName: "B"},
			},
		}
	}

	t.Run("each table once, in ID order", func(t *testing.T) {
		r := records()
		checker := must(NewVariableScopeChecker(r, r, r, r))

		require.NoError(t, checker.Check(ctx, ws, variables...))

		assert.Equal(t, [][]string{{target.ID()}}, r.targetReads)
		assert.Equal(t, [][]string{{first.ID(), second.ID()}}, r.environmentReads,
			"the environments every variable names, directly or through a target, in one read")
		require.Len(t, r.storeReads, 1)
		assert.True(t, slices.IsSorted(r.storeReads[0]))
		assert.ElementsMatch(t, []string{storeA, storeB}, r.storeReads[0])
	})

	t.Run("an archived store is refused", func(t *testing.T) {
		r := records()
		r.stores[storeB] = SecretStoreRef{ID: storeB, WorkspaceID: ws, DisplayName: "B", Archived: true}
		checker := must(NewVariableScopeChecker(r, r, r, r))

		err := checker.Check(ctx, ws, variables...)

		require.ErrorIs(t, err, ErrArchived)
		assert.Contains(t, err.Error(), "variable Api.Key")
	})
	t.Run("a refusal names the variable", func(t *testing.T) {
		second.Archive() // last, as the subtests share it
		r := records()
		checker := must(NewVariableScopeChecker(r, r, r, r))

		err := checker.Check(ctx, ws, variables...)

		require.ErrorIs(t, err, ErrArchived)
		assert.Contains(t, err.Error(), "variable Region")
	})
}
