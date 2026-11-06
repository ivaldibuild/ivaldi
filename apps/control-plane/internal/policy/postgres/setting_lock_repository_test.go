package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
)

// Records in other capabilities are plain strings: policy keeps no foreign
// keys across capabilities.
const (
	orgA = "org_a"
	orgB = "org_b"
	wsA  = "wsp_a"
	wsB  = "wsp_b"
	keyA = "delivery.default_lifecycle"
	keyB = "delivery.other"
)

// t0 has microseconds, which PostgreSQL keeps, so round trips check precision.
var t0 = time.Date(2026, 11, 2, 9, 30, 15, 123456000, time.UTC)

func newLock(t *testing.T, scope policy.LockScope, key string) *policy.SettingLock {
	t.Helper()
	l, err := policy.NewSettingLock(policy.NewSettingLockProps{Scope: scope, Key: key, LockedByPrincipalID: "prn_ada"}, nil)
	require.NoError(t, err)
	return l
}

func keysAndLevels(locks []*policy.SettingLock) []string {
	out := make([]string, 0, len(locks))
	for _, l := range locks {
		out = append(out, l.Key()+"@"+l.Scope().ResourceID())
	}
	return out
}

func TestSettingLockRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, t0)
	ctx := context.Background()
	repo := NewSettingLockRepository(db)

	orgLock := newLock(t, policy.OrganizationLockScope(orgA), keyA)
	wsLock := newLock(t, policy.WorkspaceLockScope(orgA, wsA), keyA)
	otherWS := newLock(t, policy.WorkspaceLockScope(orgA, wsB), keyB)
	otherOrg := newLock(t, policy.OrganizationLockScope(orgB), keyB)
	for _, l := range []*policy.SettingLock{orgLock, wsLock, otherWS, otherOrg} {
		require.NoError(t, repo.CreateSettingLock(ctx, l))
	}

	t.Run("round trip", func(t *testing.T) {
		got, err := repo.GetSettingLock(ctx, policy.OrganizationLockScope(orgA), keyA)
		require.NoError(t, err)
		assert.Equal(t, orgLock.Snapshot(), got.Snapshot())
		assert.Empty(t, got.PullEvents(), "a rebuilt lock records nothing")

		got, err = repo.GetSettingLock(ctx, policy.WorkspaceLockScope(orgA, wsA), keyA)
		require.NoError(t, err)
		assert.Equal(t, wsLock.Snapshot(), got.Snapshot())
	})

	t.Run("get matches exactly one level", func(t *testing.T) {
		_, err := repo.GetSettingLock(ctx, policy.WorkspaceLockScope(orgA, wsB), keyA)
		assert.ErrorIs(t, err, policy.ErrNotFound)
		_, err = repo.GetSettingLock(ctx, policy.OrganizationLockScope(orgA), keyB)
		assert.ErrorIs(t, err, policy.ErrNotFound, "a workspace lock is not the organization's")
		_, err = repo.GetSettingLock(ctx, policy.OrganizationLockScope(orgB), keyA)
		assert.ErrorIs(t, err, policy.ErrNotFound, "another organization sees nothing")
	})

	t.Run("one lock per key and level", func(t *testing.T) {
		err := repo.CreateSettingLock(ctx, newLock(t, policy.OrganizationLockScope(orgA), keyA))
		assert.ErrorIs(t, err, policy.ErrConflict, "organization locks are unique too")
		err = repo.CreateSettingLock(ctx, newLock(t, policy.WorkspaceLockScope(orgA, wsA), keyA))
		assert.ErrorIs(t, err, policy.ErrConflict)
	})

	t.Run("list at a level", func(t *testing.T) {
		got, err := repo.ListSettingLocks(ctx, policy.OrganizationLockScope(orgA))
		require.NoError(t, err)
		assert.Equal(t, []string{keyA + "@" + orgA}, keysAndLevels(got))

		got, err = repo.ListSettingLocks(ctx, policy.WorkspaceLockScope(orgA, wsB))
		require.NoError(t, err)
		assert.Equal(t, []string{keyB + "@" + wsB}, keysAndLevels(got))
	})

	t.Run("locks in effect", func(t *testing.T) {
		got, err := repo.ListLocksInEffect(ctx, policy.WorkspaceLockScope(orgA, wsA))
		require.NoError(t, err)
		assert.Equal(t, []string{keyA + "@" + orgA, keyA + "@" + wsA}, keysAndLevels(got), "the organization's and the workspace's, not another workspace's")

		got, err = repo.ListLocksInEffect(ctx, policy.OrganizationLockScope(orgA))
		require.NoError(t, err)
		assert.Equal(t, []string{keyA + "@" + orgA}, keysAndLevels(got))

		source := policy.NewLockSource(repo)
		locked, err := source.LockedKeys(ctx, orgA, wsB)
		require.NoError(t, err)
		assert.Equal(t, map[string]workspace.Level{keyA: workspace.LevelOrganization, keyB: workspace.LevelWorkspace}, locked)
	})

	t.Run("delete", func(t *testing.T) {
		require.NoError(t, repo.DeleteSettingLock(ctx, orgA, wsLock.ID()))
		_, err := repo.GetSettingLock(ctx, policy.WorkspaceLockScope(orgA, wsA), keyA)
		assert.ErrorIs(t, err, policy.ErrNotFound)

		assert.ErrorIs(t, repo.DeleteSettingLock(ctx, orgA, wsLock.ID()), policy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteSettingLock(ctx, orgA, otherOrg.ID()), policy.ErrNotFound, "another organization's lock is untouched")

		require.NoError(t, repo.CreateSettingLock(ctx, newLock(t, policy.WorkspaceLockScope(orgA, wsA), keyA)), "the key can be locked again")
	})

	t.Run("writes join the context's transaction", func(t *testing.T) {
		tx := database.NewTransactor(db)
		l := newLock(t, policy.OrganizationLockScope(orgA), "delivery.rolled_back")
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, repo.CreateSettingLock(ctx, l))
			return assert.AnError
		})
		require.ErrorIs(t, err, assert.AnError)
		_, err = repo.GetSettingLock(ctx, policy.OrganizationLockScope(orgA), "delivery.rolled_back")
		assert.ErrorIs(t, err, policy.ErrNotFound)
	})

	t.Run("the table checks the key format", func(t *testing.T) {
		err := db.Exec("INSERT INTO policy.setting_locks (id, organization_id, key, locked_by_principal_id) VALUES ('slk_x', ?, 'Not A Key', 'prn_ada')", orgA).Error
		assert.ErrorIs(t, translate(err), policy.ErrInvalid)
	})
}
