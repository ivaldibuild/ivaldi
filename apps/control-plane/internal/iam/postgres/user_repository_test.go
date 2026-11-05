package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

func TestUserRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	org := f.writeOrg(t, "acme")

	t.Run("round trip", func(t *testing.T) {
		a := f.writeAccount(t, "ada@example.com")
		u := must(iam.NewUser(iam.NewUserProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Ada", Metadata: map[string]string{"title": "Countess"}},
			AccountID:      a.ID(),
		}, nil))

		require.NoError(t, f.users.Write(ctx, u))

		got := f.loadUser(t, u.ID())
		assert.Equal(t, u.Snapshot(), got.Snapshot())
		assert.Equal(t, iam.PrincipalUser, got.Kind())
	})

	t.Run("write updates a stored user", func(t *testing.T) {
		a := f.writeAccount(t, "grace@example.com")
		u := newUser(org.ID(), a.ID())
		require.NoError(t, f.users.Write(ctx, u))
		stored := f.loadUser(t, u.ID())
		at(t, time.Hour)
		require.NoError(t, stored.Rename("Admiral Hopper"))
		require.NoError(t, stored.Suspend())

		require.NoError(t, f.users.Write(ctx, stored))

		got := f.loadUser(t, u.ID())
		assert.Equal(t, stored.Snapshot(), got.Snapshot())
		assert.Equal(t, u.JoinedAt(), got.JoinedAt(), "the membership row never changes")
	})

	t.Run("second membership in an organization", func(t *testing.T) {
		a := f.writeAccount(t, "twice@example.com")
		require.NoError(t, f.users.Write(ctx, newUser(org.ID(), a.ID())))
		principals := f.count(t, "SELECT count(*) FROM iam.principals")

		err := f.users.Write(ctx, newUser(org.ID(), a.ID()))

		assert.ErrorIs(t, err, iam.ErrAlreadyExists)
		assert.Equal(t, principals, f.count(t, "SELECT count(*) FROM iam.principals"), "the principal row is rolled back with the membership")
	})

	t.Run("membership in another organization", func(t *testing.T) {
		a := f.writeAccount(t, "both@example.com")
		other := f.writeOrg(t, "other")
		require.NoError(t, f.users.Write(ctx, newUser(org.ID(), a.ID())))

		assert.NoError(t, f.users.Write(ctx, newUser(other.ID(), a.ID())))
	})

	t.Run("account or organization that does not exist", func(t *testing.T) {
		a := f.writeAccount(t, "real@example.com")
		principals := f.count(t, "SELECT count(*) FROM iam.principals")

		assert.ErrorIs(t, f.users.Write(ctx, newUser(org.ID(), newAccount("ghost@example.com").ID())), iam.ErrNotFound)
		assert.ErrorIs(t, f.users.Write(ctx, newUser(newOrg("ghost").ID(), a.ID())), iam.ErrNotFound)
		assert.Equal(t, principals, f.count(t, "SELECT count(*) FROM iam.principals"))
	})
}

// Onboarding writes an Organization, an Account, its password, and its User
// in one transaction; the repositories must all join it.
func TestRepositoriesJoinTheContextTransaction(t *testing.T) {
	f := newFixture(t)
	tx := database.NewTransactor(f.db)
	errAbort := errors.New("abort")

	org := newOrg("acme")
	account := newAccount("ada@example.com")
	err := tx.WithinTx(f.ctx, func(ctx context.Context) error {
		require.NoError(t, f.orgs.Write(ctx, org))
		require.NoError(t, f.accounts.Write(ctx, account))
		require.NoError(t, f.passwords.Write(ctx, must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: account.ID(), PasswordHash: testHash}))))
		require.NoError(t, f.users.Write(ctx, newUser(org.ID(), account.ID())))
		return errAbort
	})

	require.ErrorIs(t, err, errAbort)
	for _, table := range []string{"organizations", "accounts", "password_credentials", "principals", "users"} {
		assert.Zero(t, f.count(t, "SELECT count(*) FROM iam."+table), table)
	}
}
