package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func TestPrincipalReaderListByIDs(t *testing.T) {
	f := newFixture(t)
	org := f.writeOrg(t, "acme")
	other := f.writeOrg(t, "globex")

	writeUser := func(organizationID, name, email string, status iam.PrincipalStatus) string {
		t.Helper()
		u := must(iam.NewUser(iam.NewUserProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: organizationID, DisplayName: name, Status: status},
			AccountID:      f.writeAccount(t, email).ID(),
		}, nil))
		require.NoError(t, f.users.Write(f.ctx, u))
		return u.ID()
	}
	ada := writeUser(org.ID(), "Ada Lovelace", "ada@example.com", iam.PrincipalActive)
	linus := writeUser(org.ID(), "Linus", "linus@example.com", iam.PrincipalSuspended)
	agent := newAgent(org.ID(), "release-agent")
	require.NoError(t, NewAgentRepository(f.db).Write(f.ctx, agent))
	outsider := writeUser(other.ID(), "Eve", "eve@example.com", iam.PrincipalActive)

	reader := NewPrincipalReader(f.db)
	found, err := reader.ListByIDs(f.ctx, org.ID(), []string{agent.ID(), "prn_missing", outsider, linus, ada})
	require.NoError(t, err)

	byID := make(map[string]iam.PrincipalSnapshot, len(found))
	for _, p := range found {
		byID[p.ID] = p
	}
	require.Len(t, byID, 3, "an unknown ID and another Organization's principal are left out")
	assert.Equal(t, iam.PrincipalAgent, byID[agent.ID()].Kind)
	assert.Equal(t, "Agent release-agent", byID[agent.ID()].DisplayName)
	assert.Equal(t, iam.PrincipalUser, byID[ada].Kind)
	assert.Equal(t, iam.PrincipalSuspended, byID[linus].Status, "the status is the principal's life")
	assert.Equal(t, org.ID(), byID[ada].OrganizationID)
	assert.NotContains(t, byID, outsider)

	t.Run("no IDs reads nothing", func(t *testing.T) {
		found, err := reader.ListByIDs(f.ctx, org.ID(), nil)
		require.NoError(t, err)
		assert.Empty(t, found)
	})
}
