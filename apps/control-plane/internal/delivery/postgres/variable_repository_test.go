package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func variableNames(vs []*delivery.Variable) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name()
	}
	return out
}

func TestVariableRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	variables := NewVariableRepository(f.db)
	project := id.New("prj")
	app := f.createApp(t, orgA, wsA, project, "billing")
	staging := f.createEnv(t, orgA, wsA, "staging")
	production := f.createEnv(t, orgA, wsA, "production")
	target := f.createTarget(t, orgA, production.ID(), "eu")

	newVariable := func(owner delivery.VariableOwner, name string, values ...delivery.NewVariableValueProps) *delivery.Variable {
		return must(delivery.NewVariable(delivery.NewVariableProps{
			OrganizationID: orgA, ProjectID: &owner.ProjectID, ApplicationID: &owner.ApplicationID,
			Name: name, Kind: delivery.VariableKindText, Values: values,
		}, nil))
	}
	inProject, inApp := delivery.VariableOwner{ProjectID: project}, delivery.VariableOwner{ApplicationID: app.ID()}

	host := must(delivery.NewVariable(delivery.NewVariableProps{
		OrganizationID: orgA, ProjectID: &project, Name: "Database.Host", Kind: delivery.VariableKindText,
		Description: new("Where the database is"), ValuesPath: new("database.host"),
		Values: []delivery.NewVariableValueProps{
			{Value: "db.internal"},
			{Value: "db.#{Region}.example.com", Scope: delivery.VariableScope{EnvironmentIDs: []string{production.ID(), staging.ID()}}},
		},
	}, nil))
	require.NoError(t, variables.CreateVariable(ctx, host))

	t.Run("round trip", func(t *testing.T) {
		got, err := variables.GetVariable(ctx, orgA, host.ID())
		require.NoError(t, err)
		assert.Equal(t, host.Snapshot(), got.Snapshot())

		_, err = variables.GetVariable(ctx, orgB, host.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound, "another organization's variable")
	})

	t.Run("a name is unique in its owner, whatever the case", func(t *testing.T) {
		err := variables.CreateVariable(ctx, newVariable(inProject, "database.HOST"))
		assert.ErrorIs(t, err, delivery.ErrSlugTaken)
		assert.NoError(t, variables.CreateVariable(ctx, newVariable(inApp, "database.HOST")), "another owner may use it")
	})

	t.Run("update replaces the values and keeps the ones that stay", func(t *testing.T) {
		v, err := variables.GetVariable(ctx, orgA, host.ID())
		require.NoError(t, err)
		kept, dropped := v.Values()[0], v.Values()[1]
		at(t, time.Hour)
		require.NoError(t, v.RemoveValue(dropped.ID))
		require.NoError(t, v.UpdateValue(kept.ID, delivery.VariableValueProps{Value: "db.local"}))
		_, err = v.AddValue(delivery.VariableValueProps{Value: "db.eu", Scope: delivery.VariableScope{TargetIDs: []string{target.ID()}}})
		require.NoError(t, err)
		require.NoError(t, v.Update(delivery.UpdateVariableProps{ValuesPath: new("")}))
		require.NoError(t, variables.UpdateVariable(ctx, v))

		got, err := variables.GetVariable(ctx, orgA, host.ID())
		require.NoError(t, err)
		assert.Equal(t, v.Snapshot(), got.Snapshot())
		assert.Equal(t, kept.CreatedAt, got.Values()[0].CreatedAt, "a kept value keeps its creation time")
	})

	t.Run("lists by owner and by project", func(t *testing.T) {
		require.NoError(t, variables.CreateVariable(ctx, newVariable(inProject, "Api.Url")))
		archivedApp := f.createApp(t, orgA, wsA, project, "legacy")
		require.NoError(t, variables.CreateVariable(ctx, newVariable(delivery.VariableOwner{ApplicationID: archivedApp.ID()}, "Legacy.Flag")))
		archivedApp.Archive()
		require.NoError(t, f.apps.UpdateApplication(ctx, archivedApp))
		elsewhere := f.createApp(t, orgA, wsA, id.New("prj"), "elsewhere")
		require.NoError(t, variables.CreateVariable(ctx, newVariable(delivery.VariableOwner{ApplicationID: elsewhere.ID()}, "Other.Flag")))
		gone := newVariable(inProject, "Gone")
		require.NoError(t, variables.CreateVariable(ctx, gone))
		gone.Archive()
		require.NoError(t, variables.UpdateVariable(ctx, gone))

		own, err := variables.ListVariables(ctx, orgA, inProject)
		require.NoError(t, err)
		assert.Equal(t, []string{"Api.Url", "Database.Host"}, variableNames(own), "active, by name")

		ofProject, ofApps, err := variables.ListVariablesInProject(ctx, orgA, project)
		require.NoError(t, err)
		assert.Equal(t, []string{"Api.Url", "Database.Host"}, variableNames(ofProject))
		require.Len(t, ofApps, 1, "only the project's active applications")
		assert.Equal(t, []string{"database.HOST"}, variableNames(ofApps[app.ID()]))

		assert.NoError(t, variables.CreateVariable(ctx, newVariable(inProject, "Gone")), "an archived variable frees its name")
	})

	t.Run("counts the active variables scoped to a record", func(t *testing.T) {
		count := func(n int, err error) int { require.NoError(t, err); return n }
		assert.Equal(t, 1, count(variables.CountActiveVariablesWithTarget(ctx, orgA, target.ID())))
		assert.Equal(t, 0, count(variables.CountActiveVariablesWithEnvironment(ctx, orgA, staging.ID())), "its value was removed")
		assert.Equal(t, 0, count(variables.CountActiveVariablesWithTarget(ctx, orgB, target.ID())))

		v, err := variables.GetVariable(ctx, orgA, host.ID())
		require.NoError(t, err)
		v.Archive()
		require.NoError(t, variables.UpdateVariable(ctx, v))
		assert.Equal(t, 0, count(variables.CountActiveVariablesWithTarget(ctx, orgA, target.ID())), "an archived variable holds nothing")
	})
}
