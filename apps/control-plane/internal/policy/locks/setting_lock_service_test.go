package locks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

func TestNewSettingLockServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(workspace.NewDefinitions(), &authorizer{})
	for i := range all {
		opts := append(append([]SettingLockServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewSettingLockService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewSettingLockService(all...)
	assert.NoError(t, err)
}

func TestTheServiceRefusesAChangeThatWasNotAuthorized(t *testing.T) {
	w := newWorld(t)
	svc := &service{locks: w.store, definitions: w.definitions, workspaces: w.store, w: change.Writer{Tx: w.store, Outbox: outboxLog{w.store}, Audit: auditLog{w.store}}}

	_, err := svc.AddSettingLock(context.Background(), ada, policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey})

	require.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Empty(t, w.Locks, "nothing is written without a decision")
}

// Each operation is authorized at the lock's level, on its IAM resource.
func TestOperationsAreAuthorizedOnTheLockLevel(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	add := policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey}
	remove := policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey}

	cases := []struct {
		name string
		call func() error
		want authorization.Request
	}{
		{"add at a workspace", func() error { _, err := w.svc.AddSettingLock(ctx, ada, add); return err }, authorization.Request{
			Actor: ada, Permission: policy.PermissionPolicyManage, OrganizationID: org, ResourceID: w.ws.ID(), Action: policy.ActionSettingLockAdd, Proposal: add,
		}},
		{"add at the organization", func() error { _, err := w.svc.AddSettingLock(ctx, ada, remove); return err }, authorization.Request{
			Actor: ada, Permission: policy.PermissionPolicyManage, OrganizationID: org, ResourceID: org, Action: policy.ActionSettingLockAdd, Proposal: remove,
		}},
		{"list", func() error { _, err := w.svc.ListSettingLocks(ctx, ada, w.wsScope()); return err }, authorization.Request{
			Actor: ada, Permission: policy.PermissionPolicyRead, OrganizationID: org, ResourceID: w.ws.ID(), Action: policy.ActionSettingLockList,
		}},
		{"remove", func() error { return w.svc.RemoveSettingLock(ctx, ada, remove) }, authorization.Request{
			Actor: ada, Permission: policy.PermissionPolicyManage, OrganizationID: org, ResourceID: org, Action: policy.ActionSettingLockRemove, Proposal: remove,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.authz.asked = nil
			require.NoError(t, tc.call())
			require.Len(t, w.authz.asked, 1)
			assert.Equal(t, tc.want, w.authz.asked[0])
		})
	}
}

func TestAddSettingLock(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	l, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey})
	require.NoError(t, err)
	assert.Equal(t, w.ws.ID(), l.WorkspaceID())
	assert.Equal(t, workspace.LevelWorkspace, l.Level())
	assert.Equal(t, ada.PrincipalID, l.LockedByPrincipalID())
	assert.Equal(t, fixedNow, l.CreatedAt())
	require.Len(t, w.Locks, 1)

	assert.Equal(t, []string{"policy.v1.setting_lock.added"}, w.eventTypes())
	assert.Equal(t, policy.AggregateSettingLock, w.Events[0].AggregateType)
	assert.Equal(t, l.ID(), w.Events[0].AggregateID)
	require.Len(t, w.AuditLog, 1)
	e := w.AuditLog[0]
	assert.Equal(t, policy.EventSettingLockAdded, e.Type)
	assert.Equal(t, l.ID(), e.ResourceID)
	assert.Equal(t, "dec-"+w.ws.ID(), e.DecisionID, "the audit record carries the decision that allowed it")
	assert.Equal(t, l.Snapshot(), e.After)
	assert.Nil(t, e.Before)

	t.Run("adding it again changes nothing", func(t *testing.T) {
		w.reset()
		again, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey})
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), again.Snapshot())
		assert.Empty(t, w.AuditLog)
		assert.Empty(t, w.Events)
	})

	t.Run("the organization can lock the same key", func(t *testing.T) {
		org, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey})
		require.NoError(t, err)
		assert.Equal(t, workspace.LevelOrganization, org.Level())
		assert.Len(t, w.Locks, 2)
	})
}

func TestAddSettingLockIsRefused(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	tests := []struct {
		name         string
		in           policy.SettingLockInput
		wantErr      error
		wantNotAsked bool
	}{
		{"an unknown key", policy.SettingLockInput{Scope: w.orgScope(), Key: "delivery.nope"}, policy.ErrInvalid, true},
		{"a key not settable below the lock", policy.SettingLockInput{Scope: w.wsScope(), Key: workspaceOnlyKey}, policy.ErrInvalid, true},
		{"no organization", policy.SettingLockInput{Key: lifecycleKey}, policy.ErrInvalid, true},
		{"a workspace that does not exist", policy.SettingLockInput{Scope: policy.WorkspaceLockScope(org, "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"), Key: lifecycleKey}, policy.ErrNotFound, false},
		{"another organization's workspace", policy.SettingLockInput{Scope: policy.WorkspaceLockScope("org_other", w.ws.ID()), Key: lifecycleKey}, policy.ErrNotFound, false},
		{"an archived workspace", policy.SettingLockInput{Scope: policy.WorkspaceLockScope(org, w.archived.ID()), Key: lifecycleKey}, policy.ErrArchived, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			_, err := w.svc.AddSettingLock(ctx, ada, tt.in)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, w.Locks)
			assert.Empty(t, w.AuditLog)
			assert.Empty(t, w.Events)
			if tt.wantNotAsked {
				assert.Empty(t, w.authz.asked, "refused before asking")
			}
		})
	}

	t.Run("denied", func(t *testing.T) {
		w.reset()
		w.authz.decide = deny
		defer func() { w.authz.decide = nil }()
		_, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey})
		require.ErrorIs(t, err, authorization.ErrDenied)
		assert.Empty(t, w.Locks)
	})
}

func TestRemoveSettingLock(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	in := policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey}

	err := w.svc.RemoveSettingLock(ctx, ada, in)
	require.ErrorIs(t, err, policy.ErrNotFound)

	l, err := w.svc.AddSettingLock(ctx, ada, in)
	require.NoError(t, err)
	w.reset()

	require.NoError(t, w.svc.RemoveSettingLock(ctx, ada, in))
	assert.Empty(t, w.Locks, "the row is deleted; the audit record keeps it")
	assert.Equal(t, []string{"policy.v1.setting_lock.removed"}, w.eventTypes())
	require.Len(t, w.AuditLog, 1)
	e := w.AuditLog[0]
	assert.Equal(t, policy.EventSettingLockRemoved, e.Type)
	assert.Equal(t, l.ID(), e.ResourceID)
	assert.Equal(t, l.Snapshot(), e.Before)
	assert.Nil(t, e.After)
	assert.Equal(t, "dec-"+w.ws.ID(), e.DecisionID)

	t.Run("a lock whose setting is no longer defined can be removed", func(t *testing.T) {
		stale, err := policy.NewSettingLock(policy.NewSettingLockProps{Scope: w.orgScope(), Key: "delivery.retired", LockedByPrincipalID: "prn_ada"}, nil)
		require.NoError(t, err)
		require.NoError(t, w.CreateSettingLock(ctx, stale))
		require.NoError(t, w.svc.RemoveSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: "delivery.retired"}))
		assert.Empty(t, w.Locks)
	})

	t.Run("a lock in an archived workspace can be removed", func(t *testing.T) {
		archivedLock, err := policy.NewSettingLock(policy.NewSettingLockProps{Scope: policy.WorkspaceLockScope(org, w.archived.ID()), Key: lifecycleKey, LockedByPrincipalID: "prn_ada"}, nil)
		require.NoError(t, err)
		require.NoError(t, w.CreateSettingLock(ctx, archivedLock))
		require.NoError(t, w.svc.RemoveSettingLock(ctx, ada, policy.SettingLockInput{Scope: archivedLock.Scope(), Key: lifecycleKey}))
	})
}

func TestListSettingLocks(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	_, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey})
	require.NoError(t, err)
	_, err = w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: workspaceOnlyKey})
	require.NoError(t, err)
	_, err = w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey})
	require.NoError(t, err)

	got, err := w.svc.ListSettingLocks(ctx, ada, w.orgScope())
	require.NoError(t, err)
	require.Len(t, got, 2, "only the locks set at exactly that level")
	assert.Equal(t, lifecycleKey, got[0].Key())
	assert.Equal(t, workspaceOnlyKey, got[1].Key())

	got, err = w.svc.ListSettingLocks(ctx, ada, w.wsScope())
	require.NoError(t, err)
	require.Len(t, got, 1)

	w.authz.decide = deny
	_, err = w.svc.ListSettingLocks(ctx, ada, w.orgScope())
	require.ErrorIs(t, err, authorization.ErrDenied)
}

func TestAgentLockChangesNeedApproval(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	in := policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey}

	_, err := w.svc.AddSettingLock(ctx, agent, in)
	require.ErrorIs(t, err, authorization.ErrDenied, "an allow without a human approver is refused anyway")
	assert.Empty(t, w.Locks)

	w.authz.decide = func(authorization.Request) authorization.Decision {
		return authorization.Decision{ID: "dec_approved", Outcome: authorization.OutcomeAllow, ApprovedByPrincipalID: "prn_ada"}
	}
	_, err = w.svc.AddSettingLock(ctx, agent, in)
	require.NoError(t, err)
	assert.Equal(t, "prn_ada", w.AuditLog[0].ApprovedByPrincipalID)

	w.authz.decide = nil
	err = w.svc.RemoveSettingLock(ctx, agent, in)
	require.ErrorIs(t, err, authorization.ErrDenied, "removing a lock needs approval too")
	assert.Len(t, w.Locks, 1)
}

// The locks the service adds are the ones the workspace capability sees
// through the lock source.
func TestAddedLocksReachTheLockSource(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	_, err := w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.wsScope(), Key: lifecycleKey})
	require.NoError(t, err)

	source := policy.NewLockSource(w.store)
	locked, err := source.LockedKeys(ctx, org, w.ws.ID())
	require.NoError(t, err)
	assert.Equal(t, map[string]workspace.Level{lifecycleKey: workspace.LevelWorkspace}, locked)

	_, err = w.svc.AddSettingLock(ctx, ada, policy.SettingLockInput{Scope: w.orgScope(), Key: lifecycleKey})
	require.NoError(t, err)
	locked, err = source.LockedKeys(ctx, org, w.ws.ID())
	require.NoError(t, err)
	assert.Equal(t, map[string]workspace.Level{lifecycleKey: workspace.LevelOrganization}, locked, "the organization's lock wins")
}
