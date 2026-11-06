package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func TestServiceAccountAndAPITokenRepositories(t *testing.T) {
	f := newFixture(t)
	at(t, 0)
	org := f.writeOrg(t, "acme")
	ada := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, f.users.Write(f.ctx, ada))
	accounts := NewServiceAccountRepository(f.db)
	tokens := NewAPITokenRepository(f.db)

	newSA := func(name string) *iam.ServiceAccount {
		sa := must(iam.NewServiceAccount(iam.NewServiceAccountProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: name},
			Description:    name + " runs releases", CreatedByPrincipalID: ada.ID(),
		}, nil))
		require.NoError(t, accounts.Write(f.ctx, sa))
		return sa
	}
	gha := newSA("GitHub Actions")
	gitlab := newSA("GitLab CI")

	got, err := accounts.Get(f.ctx, org.ID(), gha.ID())
	require.NoError(t, err)
	assert.Equal(t, gha.Snapshot(), got.Snapshot())
	_, err = accounts.Get(f.ctx, org.ID(), ada.ID())
	assert.ErrorIs(t, err, iam.ErrNotFound, "a user is not a service account")

	at(t, time.Minute)
	require.NoError(t, gitlab.Suspend())
	require.NoError(t, accounts.Write(f.ctx, gitlab))
	active, err := accounts.List(f.ctx, org.ID(), false)
	require.NoError(t, err)
	assert.Equal(t, []string{gha.ID()}, idsOf(active))
	all, err := accounts.List(f.ctx, org.ID(), true)
	require.NoError(t, err)
	assert.Equal(t, []string{gha.ID(), gitlab.ID()}, idsOf(all), "by name")

	issued, err := iam.NewAPITokenSecret()
	require.NoError(t, err)
	expires := t0.Add(24 * time.Hour)
	tok := must(iam.NewAPIToken(iam.NewAPITokenProps{
		OrganizationID: org.ID(), PrincipalID: gha.ID(), Name: "github-actions",
		TokenPrefix: issued.Prefix, TokenHash: issued.Hash, ExpiresAt: &expires,
	}, nil))
	require.NoError(t, tokens.Write(f.ctx, tok))

	byHash, err := tokens.GetByHash(f.ctx, iam.HashAPIToken(issued.Secret))
	require.NoError(t, err)
	assert.Equal(t, tok.Snapshot(), byHash.Snapshot())
	_, err = tokens.GetByHash(f.ctx, iam.HashAPIToken(issued.Secret+"x"))
	assert.ErrorIs(t, err, iam.ErrNotFound)

	at(t, 2*time.Minute)
	tok.RecordUse()
	tok.Revoke()
	require.NoError(t, tokens.Write(f.ctx, tok))
	got2, err := tokens.Get(f.ctx, org.ID(), tok.ID())
	require.NoError(t, err)
	assert.Equal(t, tok.Snapshot(), got2.Snapshot(), "last use and revocation are written")

	listed, err := tokens.ListByPrincipal(f.ctx, org.ID(), gha.ID())
	require.NoError(t, err)
	assert.Equal(t, []string{tok.ID()}, idsOf(listed))
	_, err = tokens.Get(f.ctx, f.writeOrg(t, "globex").ID(), tok.ID())
	assert.ErrorIs(t, err, iam.ErrNotFound, "not another Organization's")
}
