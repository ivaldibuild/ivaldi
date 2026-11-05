package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func TestAccountRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		a := newAccount("ada@example.com")
		a.VerifyEmail()
		require.NoError(t, f.accounts.Write(ctx, a))

		assert.Equal(t, a.Snapshot(), f.loadAccount(t, a.ID()).Snapshot())
	})

	t.Run("exists by email ignoring case", func(t *testing.T) {
		f.writeAccount(t, "Grace@Example.com")

		exists, err := f.accounts.ExistsByEmail(ctx, "grace@example.COM")
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = f.accounts.ExistsByEmail(ctx, "nobody@example.com")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("email held by another account", func(t *testing.T) {
		f.writeAccount(t, "taken@example.com")

		err := f.accounts.Write(ctx, newAccount("TAKEN@example.com"))

		assert.ErrorIs(t, err, iam.ErrAlreadyExists)
	})

	t.Run("write updates a stored account", func(t *testing.T) {
		a := f.writeAccount(t, "before@example.com")
		stored := f.loadAccount(t, a.ID())
		at(t, time.Hour)
		require.NoError(t, stored.Rename("After"))
		require.NoError(t, stored.ChangeEmail("after@example.com"))
		require.NoError(t, stored.Suspend())

		require.NoError(t, f.accounts.Write(ctx, stored))

		got := f.loadAccount(t, a.ID())
		assert.Equal(t, stored.Snapshot(), got.Snapshot())
		assert.Equal(t, a.CreatedAt(), got.CreatedAt(), "creation time never changes")
	})
}

func TestPasswordCredentialRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		a := f.writeAccount(t, "ada@example.com")
		c := must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: a.ID(), PasswordHash: testHash}))

		require.NoError(t, f.passwords.Write(ctx, c))

		assert.Equal(t, c.Snapshot(), f.loadPassword(t, a.ID()).Snapshot())
	})

	t.Run("write replaces the password", func(t *testing.T) {
		a := f.writeAccount(t, "grace@example.com")
		require.NoError(t, f.passwords.Write(ctx, must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: a.ID(), PasswordHash: testHash}))))
		stored := f.loadPassword(t, a.ID())
		at(t, time.Hour)
		require.NoError(t, stored.Change(testHash+"new"))

		require.NoError(t, f.passwords.Write(ctx, stored))

		assert.Equal(t, stored.Snapshot(), f.loadPassword(t, a.ID()).Snapshot())
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.password_credentials WHERE account_id = ?", a.ID()))
	})

	t.Run("account that does not exist", func(t *testing.T) {
		missing := newAccount("ghost@example.com")
		c := must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: missing.ID(), PasswordHash: testHash}))

		assert.ErrorIs(t, f.passwords.Write(ctx, c), iam.ErrNotFound)
	})
}
