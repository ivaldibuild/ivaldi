package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// Each count fake embeds its repository interface, so a call to anything
// but its count panics.
type countEnvironments struct {
	EnvironmentRepository
	n   map[string]int
	err error
}

func (c countEnvironments) CountActiveEnvironments(_ context.Context, _, workspaceID string) (int, error) {
	return c.n[workspaceID], c.err
}

type countLifecycles struct {
	LifecycleRepository
	n map[string]int
}

func (c countLifecycles) CountActiveLifecycles(_ context.Context, _, workspaceID string) (int, error) {
	return c.n[workspaceID], nil
}

type countRegistries struct {
	RegistryRepository
	n map[string]int
}

func (c countRegistries) CountActiveRegistries(_ context.Context, _, workspaceID string) (int, error) {
	return c.n[workspaceID], nil
}

type countApplications struct {
	ApplicationRepository
	n map[string]int
}

func (c countApplications) CountActiveApplications(_ context.Context, _, projectID string) (int, error) {
	return c.n[projectID], nil
}

func TestNewDependents(t *testing.T) {
	ctx := context.Background()
	const ws, other, project = "wsp_1", "wsp_2", "prj_1"
	envs := countEnvironments{n: map[string]int{ws: 2}}
	deps, err := NewDependents(envs, countLifecycles{n: map[string]int{ws: 1}}, countRegistries{n: map[string]int{ws: 3}}, countApplications{n: map[string]int{project: 4}})
	require.NoError(t, err)

	t.Run("a project counts its applications", func(t *testing.T) {
		got, err := deps.ActiveDependents(ctx, workspace.ArchiveTarget{OrganizationID: "org", WorkspaceID: ws, ProjectID: project})
		require.NoError(t, err)
		assert.Equal(t, []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: 4}}, got)
	})

	t.Run("a workspace counts its environments, lifecycles, and registries", func(t *testing.T) {
		got, err := deps.ActiveDependents(ctx, workspace.ArchiveTarget{OrganizationID: "org", WorkspaceID: ws})
		require.NoError(t, err)
		assert.Equal(t, []workspace.Dependency{
			{Capability: "delivery", Kind: "Environment", Count: 2},
			{Capability: "delivery", Kind: "Lifecycle", Count: 1},
			{Capability: "delivery", Kind: "Registry", Count: 3},
		}, got)
	})

	t.Run("another workspace has none", func(t *testing.T) {
		got, err := deps.ActiveDependents(ctx, workspace.ArchiveTarget{OrganizationID: "org", WorkspaceID: other})
		require.NoError(t, err)
		for _, d := range got {
			assert.Zero(t, d.Count, d.Kind)
		}
	})

	t.Run("a failed count is an error", func(t *testing.T) {
		boom := errors.New("boom")
		failing, err := NewDependents(countEnvironments{err: boom}, countLifecycles{}, countRegistries{}, countApplications{})
		require.NoError(t, err)
		_, err = failing.ActiveDependents(ctx, workspace.ArchiveTarget{OrganizationID: "org", WorkspaceID: ws})
		require.ErrorIs(t, err, boom)
	})

	t.Run("every repository is required", func(t *testing.T) {
		_, err := NewDependents(nil, countLifecycles{}, countRegistries{}, countApplications{})
		assert.Error(t, err)
		_, err = NewDependents(envs, nil, countRegistries{}, countApplications{})
		assert.Error(t, err)
		_, err = NewDependents(envs, countLifecycles{}, nil, countApplications{})
		assert.Error(t, err)
		_, err = NewDependents(envs, countLifecycles{}, countRegistries{}, nil)
		assert.Error(t, err)
	})
}

type ownedApplications map[string]int

func (o ownedApplications) CountActiveApplicationsOwnedBy(_ context.Context, _, teamID string) (int, error) {
	return o[teamID], nil
}

func TestNewTeamOwnership(t *testing.T) {
	_, err := NewTeamOwnership(nil)
	require.Error(t, err)

	owner, err := NewTeamOwnership(ownedApplications{"tem_1": 2})
	require.NoError(t, err)
	got, err := owner.ActiveOwned(context.Background(), "org", "tem_1")
	require.NoError(t, err)
	assert.Equal(t, []team.Owned{{Capability: "delivery", Kind: "Application", Count: 2}}, got)
}
