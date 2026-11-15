package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// The taker snapshots what the Application deploys with as the Release's
// next revision, and a later change to a variable leaves the revision alone.
func TestReleaseVariablesTaker(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	variables, revisions := NewVariableRepository(f.db), NewReleaseVariablesRepository(f.db)
	var locked []string
	taker := must(delivery.NewReleaseVariablesTaker(variables, revisions, lockerFunc(func(key string) { locked = append(locked, key) })))
	project := id.New("prj")
	app := f.createApp(t, orgA, wsA, project, "billing")
	lifecycle := f.createLifecycle(t, orgA, wsA, "standard")
	channel := f.createChannel(t, orgA, app.ID(), lifecycle.ID(), "default", true)
	artifact := f.createArtifact(t, orgA, app.ID(), "api")
	build := newBuild(orgA, "ghcr.io/acme/api", digest("1"), delivery.BuildDetails{}, delivery.BuildCIReported)
	_, err := NewBuildRepository(f.db).CreateBuild(ctx, build)
	require.NoError(t, err)
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: orgA, ApplicationID: app.ID(), ChannelID: channel.ID(), Version: "1.0.0",
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: artifact.ID(), BuildID: build.ID()}},
	}, nil))
	require.NoError(t, NewReleaseRepository(f.db).CreateRelease(ctx, rel))

	region := must(delivery.NewVariable(delivery.NewVariableProps{
		OrganizationID: orgA, ProjectID: &project, Name: "Region", Kind: delivery.VariableKindText,
		Values: []delivery.NewVariableValueProps{{Value: "eu-west-1"}},
	}, nil))
	level := must(delivery.NewVariable(delivery.NewVariableProps{
		OrganizationID: orgA, ApplicationID: new(app.ID()), Name: "Log.Level", Kind: delivery.VariableKindText,
		ValuesPath: new("log.level"), Values: []delivery.NewVariableValueProps{{Value: "info"}},
	}, nil))
	require.NoError(t, variables.CreateVariable(ctx, region))
	require.NoError(t, variables.CreateVariable(ctx, level))

	_, err = revisions.GetCurrentReleaseVariables(ctx, orgA, rel.ID())
	require.ErrorIs(t, err, delivery.ErrNotFound, "a release has no revision until one is taken")

	taken, err := taker.Take(ctx, rel, project, new("prn_ada"))
	require.NoError(t, err)
	require.Len(t, taken, 1)
	assert.Equal(t, []string{delivery.VariablesLock(project)}, locked, "the two reads see the variables as they stood together")
	first := taken[0]
	assert.Equal(t, 1, first.Revision())
	assert.Equal(t, []delivery.SnapshotVariable{
		{Name: "Log.Level", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText, ValuesPath: new("log.level"),
			Values: []delivery.SnapshotValue{{Value: "info", Scope: delivery.VariableScope{EnvironmentIDs: []string{}, TargetIDs: []string{}, ChannelIDs: []string{}}}}},
		{Name: "Region", Source: delivery.VariableSourceProject, Kind: delivery.VariableKindText,
			Values: []delivery.SnapshotValue{{Value: "eu-west-1", Scope: delivery.VariableScope{EnvironmentIDs: []string{}, TargetIDs: []string{}, ChannelIDs: []string{}}}}},
	}, first.Variables(), "the application's variables, then its project's")

	stored, err := revisions.GetCurrentReleaseVariables(ctx, orgA, rel.ID())
	require.NoError(t, err)
	assert.Equal(t, first.Snapshot(), stored.Snapshot())

	// The variable changes; the Release keeps what it was released with
	// until its variables are taken again.
	at(t, time.Hour)
	require.NoError(t, level.UpdateValue(level.Values()[0].ID, delivery.VariableValueProps{Value: "debug"}))
	require.NoError(t, variables.UpdateVariable(ctx, level))
	stored, err = revisions.GetCurrentReleaseVariables(ctx, orgA, rel.ID())
	require.NoError(t, err)
	assert.Equal(t, "info", stored.Variables()[0].Values[0].Value)

	taken, err = taker.Take(ctx, rel, project, nil)
	require.NoError(t, err)
	require.Len(t, taken, 2, "the revision it replaces, then the new one")
	assert.Equal(t, delivery.ReleaseVariablesSuperseded, taken[0].Status())
	assert.Equal(t, 2, taken[1].Revision())
	assert.Equal(t, "debug", taken[1].Variables()[0].Values[0].Value)

	current, err := revisions.GetCurrentReleaseVariables(ctx, orgA, rel.ID())
	require.NoError(t, err)
	assert.Equal(t, taken[1].ID(), current.ID())
	old, err := revisions.GetReleaseVariables(ctx, orgA, first.ID())
	require.NoError(t, err)
	assert.Equal(t, delivery.ReleaseVariablesSuperseded, old.Status(), "kept for the deployments that used it")
	_, err = revisions.GetReleaseVariables(ctx, orgB, first.ID())
	assert.ErrorIs(t, err, delivery.ErrNotFound)
}

// lockerFunc records the locks a test's subject takes; the database's own
// Locker needs a transaction.
type lockerFunc func(key string)

func (f lockerFunc) Lock(_ context.Context, key string) error {
	f(key)
	return nil
}
