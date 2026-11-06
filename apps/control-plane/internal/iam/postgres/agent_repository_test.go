package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func newAgent(orgID, slug string) *iam.Agent {
	return must(iam.NewAgent(iam.NewAgentProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: orgID, DisplayName: "Agent " + slug},
		Slug:           slug,
	}, nil))
}

func TestAgentRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	agents := NewAgentRepository(f.db)
	org := f.writeOrg(t, "acme")
	other := f.writeOrg(t, "other")

	t.Run("round trip", func(t *testing.T) {
		a := must(iam.NewAgent(iam.NewAgentProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Release agent", Metadata: map[string]string{"tier": "1"}},
			Slug:           "release-agent",
		}, nil))
		require.NoError(t, agents.Write(ctx, a))

		got, err := agents.Get(ctx, org.ID(), a.ID())
		require.NoError(t, err)
		assert.Equal(t, a.Snapshot(), got.Snapshot())
		assert.Equal(t, iam.PrincipalAgent, got.Kind())
		assert.Empty(t, got.PullEvents(), "a loaded agent records no event")

		bySlug, err := agents.GetBySlug(ctx, org.ID(), "release-agent")
		require.NoError(t, err)
		assert.Equal(t, a.Snapshot(), bySlug.Snapshot())
	})

	t.Run("write updates a stored agent", func(t *testing.T) {
		a := newAgent(org.ID(), "triage-agent")
		require.NoError(t, agents.Write(ctx, a))
		stored, err := agents.Get(ctx, org.ID(), a.ID())
		require.NoError(t, err)
		at(t, time.Hour)
		require.NoError(t, stored.Rename("Triage"))
		require.NoError(t, stored.Suspend())

		require.NoError(t, agents.Write(ctx, stored))

		got, err := agents.Get(ctx, org.ID(), a.ID())
		require.NoError(t, err)
		assert.Equal(t, stored.Snapshot(), got.Snapshot())
	})

	t.Run("slugs are unique among an organization's agents", func(t *testing.T) {
		require.NoError(t, agents.Write(ctx, newAgent(org.ID(), "clash")))
		principals := f.count(t, "SELECT count(*) FROM iam.principals")

		assert.ErrorIs(t, agents.Write(ctx, newAgent(org.ID(), "clash")), iam.ErrAlreadyExists)
		assert.Equal(t, principals, f.count(t, "SELECT count(*) FROM iam.principals"), "the principal row is rolled back with the agent")
		assert.NoError(t, agents.Write(ctx, newAgent(other.ID(), "clash")), "another organization may use the slug")
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		a := newAgent(org.ID(), "isolated")
		require.NoError(t, agents.Write(ctx, a))

		_, err := agents.Get(ctx, other.ID(), a.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound)
		_, err = agents.GetBySlug(ctx, other.ID(), "isolated")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("a user is not an agent", func(t *testing.T) {
		u := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
		require.NoError(t, f.users.Write(ctx, u))
		_, err := agents.Get(ctx, org.ID(), u.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("an organization that does not exist", func(t *testing.T) {
		assert.ErrorIs(t, agents.Write(ctx, newAgent(newOrg("ghost").ID(), "ghost")), iam.ErrNotFound)
	})
}

func TestPrincipalRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	principals := NewPrincipalRepository(f.db)
	agents := NewAgentRepository(f.db)
	org := f.writeOrg(t, "acme")
	other := f.writeOrg(t, "other")

	u := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, u.Suspend())
	require.NoError(t, f.users.Write(ctx, u))
	a := newAgent(org.ID(), "release-agent")
	require.NoError(t, agents.Write(ctx, a))

	got, err := principals.Get(ctx, org.ID(), u.ID())
	require.NoError(t, err)
	assert.Equal(t, u.Snapshot().PrincipalSnapshot, got)
	assert.Equal(t, iam.PrincipalSuspended, got.Status)

	got, err = principals.Get(ctx, org.ID(), a.ID())
	require.NoError(t, err)
	assert.Equal(t, a.Snapshot().PrincipalSnapshot, got)
	assert.Equal(t, iam.PrincipalAgent, got.Kind)

	_, err = principals.Get(ctx, other.ID(), a.ID())
	assert.ErrorIs(t, err, iam.ErrNotFound, "another organization sees nothing")
	_, err = principals.Get(ctx, org.ID(), "prn_01JZ0000000000000000000099")
	assert.ErrorIs(t, err, iam.ErrNotFound)
}
