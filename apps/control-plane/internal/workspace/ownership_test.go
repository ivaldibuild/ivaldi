package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
)

type ownedProjects map[string]int

func (o ownedProjects) CountActiveProjectsOwnedBy(_ context.Context, _, teamID string) (int, error) {
	return o[teamID], nil
}

func TestNewTeamOwnership(t *testing.T) {
	_, err := NewTeamOwnership(nil)
	require.Error(t, err)

	owner, err := NewTeamOwnership(ownedProjects{"tem_1": 3})
	require.NoError(t, err)
	got, err := owner.ActiveOwned(context.Background(), "org", "tem_1")
	require.NoError(t, err)
	assert.Equal(t, []team.Owned{{Capability: "workspace", Kind: "Project", Count: 3}}, got)
}
