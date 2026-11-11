package team

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type memberships struct {
	Directory
	byPrincipal map[string][]Membership
	err         error
}

func (m memberships) ListTeamsForMembers(context.Context, string, []string) (map[string][]Membership, error) {
	return m.byPrincipal, m.err
}

func TestPrincipalTeamSource(t *testing.T) {
	archivedAt := time.Now()
	src, err := NewPrincipalTeamSource(memberships{byPrincipal: map[string][]Membership{
		"prn_agent": {
			{TeamID: "tem_platform", Slug: "platform", DisplayName: "Platform", Role: RoleLead},
			{TeamID: "tem_old", Slug: "old", DisplayName: "Old", ArchivedAt: &archivedAt},
		},
		"prn_retired": {{TeamID: "tem_old", Slug: "old", DisplayName: "Old", ArchivedAt: &archivedAt}},
	}})
	require.NoError(t, err)

	teams, err := src.ListActiveTeams(context.Background(), "org_1", []string{"prn_agent", "prn_retired"})
	require.NoError(t, err)
	assert.Equal(t, map[string][]iam.PrincipalTeam{
		"prn_agent": {{ID: "tem_platform", Slug: "platform", DisplayName: "Platform"}},
	}, teams, "archived Teams are left out")

	t.Run("a read failure is returned", func(t *testing.T) {
		boom := errors.New("teams unavailable")
		src, err := NewPrincipalTeamSource(memberships{err: boom})
		require.NoError(t, err)
		_, err = src.ListActiveTeams(context.Background(), "org_1", []string{"prn_agent"})
		assert.ErrorIs(t, err, boom)
	})
}
