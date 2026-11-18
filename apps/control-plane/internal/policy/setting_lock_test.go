package policy

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const (
	testOrg      = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	lifecycleKey = "delivery.default_lifecycle"
)

var t0 = time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)

func validProps() NewSettingLockProps {
	return NewSettingLockProps{Scope: WorkspaceLockScope(testOrg, "wsp_1"), Key: lifecycleKey, LockedByPrincipalID: "prn_ada"}
}

func TestNewSettingLock(t *testing.T) {
	clock.Fix(t, t0)

	l, err := NewSettingLock(validProps(), nil)
	require.NoError(t, err)
	assert.True(t, id.Valid("slk", l.ID()), l.ID())
	assert.Equal(t, workspace.LevelWorkspace, l.Level())
	assert.Equal(t, "wsp_1", l.Scope().ResourceID())
	assert.Equal(t, t0, l.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, []aggregate.Event{SettingLockAdded{l.Snapshot()}}, l.PullEvents())

	org, err := NewSettingLock(NewSettingLockProps{Scope: OrganizationLockScope(testOrg), Key: lifecycleKey, LockedByPrincipalID: "prn_ada"}, nil)
	require.NoError(t, err)
	assert.Equal(t, workspace.LevelOrganization, org.Level())
	assert.Equal(t, testOrg, org.Scope().ResourceID(), "an organization lock is authorized on the organization")

	stored := l.ID()
	created := t0.Add(-time.Hour)
	again, err := NewSettingLock(NewSettingLockProps{Scope: l.Scope(), Key: l.Key(), LockedByPrincipalID: "prn_ada", CreatedAt: &created}, &stored)
	require.NoError(t, err)
	assert.Equal(t, created, again.CreatedAt())
	assert.Empty(t, again.PullEvents(), "a stored lock records nothing")

	t.Run("is refused", func(t *testing.T) {
		bad := "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
		cases := map[string]struct {
			props NewSettingLockProps
			id    *string
		}{
			"without an organization":   {props: NewSettingLockProps{Key: lifecycleKey, LockedByPrincipalID: "prn_ada"}},
			"with a key not namespaced": {props: NewSettingLockProps{Scope: OrganizationLockScope(testOrg), Key: "lifecycle", LockedByPrincipalID: "prn_ada"}},
			"without who locked it":     {props: NewSettingLockProps{Scope: OrganizationLockScope(testOrg), Key: lifecycleKey}},
			"with another type's ID":    {props: validProps(), id: &bad},
		}
		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				_, err := NewSettingLock(c.props, c.id)
				assert.ErrorIs(t, err, ErrInvalid)
			})
		}
	})
}

func TestRemoveRecordsTheRemoval(t *testing.T) {
	l, err := NewSettingLock(validProps(), nil)
	require.NoError(t, err)
	l.PullEvents()

	l.Remove("prn_bob")
	assert.Equal(t, []aggregate.Event{SettingLockRemoved{SettingLock: l.Snapshot(), RemovedByPrincipalID: "prn_bob"}}, l.PullEvents())
}

func TestEventNames(t *testing.T) {
	assert.Equal(t, "policy.v1.setting_lock.added", SettingLockAdded{}.EventName())
	assert.Equal(t, "policy.v1.setting_lock.removed", SettingLockRemoved{}.EventName())
}

func TestSnapshotJSONKeys(t *testing.T) {
	tests := []struct {
		value any
		keys  []string
	}{
		{SettingLockSnapshot{}, []string{"id", "organization_id", "workspace_id", "level", "key", "locked_by_principal_id", "created_at"}},
		{SettingLockAdded{}, []string{"setting_lock"}},
		{SettingLockRemoved{}, []string{"setting_lock", "removed_by_principal_id"}},
		{ApprovalRequestSnapshot{}, []string{
			"id", "organization_id", "subject_type", "subject_id", "title", "state", "decision_id", "rule", "reason",
			"action", "permission", "resource_id", "proposal_digest", "summary", "read_resource_id", "owner_team_id",
			"requested_by_principal_id", "session_id", "on_behalf_of_principal_id", "requirements",
			"closed_by_principal_id", "closed_decision_id", "note", "closed_at", "created_at", "updated_at",
		}},
		{Requirement{}, []string{"permission", "resource_id", "resource_path"}},
		{ApprovalRequestCreated{}, []string{"approval_request"}},
		{ApprovalRequestApproved{}, []string{"approval_request"}},
		{ApprovalRequestRejected{}, []string{"approval_request"}},
		{ApprovalRequestCancelled{}, []string{"approval_request"}},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.value)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &fields))
		assert.ElementsMatch(t, tt.keys, slices.Collect(maps.Keys(fields)), "%T", tt.value)
	}
}

func TestValidateLockKey(t *testing.T) {
	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(workspace.Definition{Key: lifecycleKey, Levels: []workspace.Level{workspace.LevelWorkspace, workspace.LevelProject}, Permission: "lifecycle.manage"}))
	require.NoError(t, defs.Register(workspace.Definition{Key: "delivery.workspace_only", Levels: []workspace.Level{workspace.LevelWorkspace}, Permission: "p"}))
	require.NoError(t, defs.Register(workspace.Definition{Key: "iam.org_only", Levels: []workspace.Level{workspace.LevelOrganization}, Permission: "p"}))

	orgScope, wsScope := OrganizationLockScope(testOrg), WorkspaceLockScope(testOrg, "wsp_1")
	assert.NoError(t, ValidateLockKey(defs, orgScope, lifecycleKey))
	assert.NoError(t, ValidateLockKey(defs, wsScope, lifecycleKey))
	assert.NoError(t, ValidateLockKey(defs, orgScope, "delivery.workspace_only"), "an organization can lock what its workspaces set")

	for name, c := range map[string]struct {
		scope LockScope
		key   string
	}{
		"an unknown key": {orgScope, "delivery.nope"},
		"a key set only at the workspace's level": {wsScope, "delivery.workspace_only"},
		"a key set only at the organization":      {orgScope, "iam.org_only"},
		"no organization":                         {LockScope{}, lifecycleKey},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, ValidateLockKey(defs, c.scope, c.key), ErrInvalid)
		})
	}
}

// locksInEffect returns the locks it holds that apply at a scope, as the
// repository does.
type locksInEffect []*SettingLock

func (ls locksInEffect) ListLocksInEffect(_ context.Context, scope LockScope) ([]*SettingLock, error) {
	var out []*SettingLock
	for _, l := range ls {
		if l.OrganizationID() == scope.OrganizationID && (l.WorkspaceID() == "" || l.WorkspaceID() == scope.WorkspaceID) {
			out = append(out, l)
		}
	}
	return out, nil
}

func TestLockSource(t *testing.T) {
	lock := func(scope LockScope, key string) *SettingLock {
		l, err := NewSettingLock(NewSettingLockProps{Scope: scope, Key: key, LockedByPrincipalID: "prn_ada"}, nil)
		require.NoError(t, err)
		return l
	}
	orgScope := OrganizationLockScope(testOrg)
	// The Workspace lock comes after the Organization's on the same key, and
	// before it on another, so the order of the rows does not matter.
	source := NewLockSource(locksInEffect{
		lock(WorkspaceLockScope(testOrg, "wsp_1"), "delivery.both_first"),
		lock(orgScope, "delivery.both_first"),
		lock(orgScope, lifecycleKey),
		lock(orgScope, "delivery.both_last"),
		lock(WorkspaceLockScope(testOrg, "wsp_1"), "delivery.both_last"),
		lock(WorkspaceLockScope(testOrg, "wsp_1"), "delivery.workspace"),
		lock(WorkspaceLockScope(testOrg, "wsp_2"), "delivery.other"),
		lock(OrganizationLockScope("org_other"), "delivery.elsewhere"),
	})
	ctx := context.Background()

	got, err := source.LockedKeys(ctx, testOrg, "wsp_1")
	require.NoError(t, err)
	assert.Equal(t, map[string]workspace.Level{
		lifecycleKey:          workspace.LevelOrganization,
		"delivery.both_first": workspace.LevelOrganization,
		"delivery.both_last":  workspace.LevelOrganization,
		"delivery.workspace":  workspace.LevelWorkspace,
	}, got, "an organization lock wins")

	got, err = source.LockedKeys(ctx, testOrg, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]workspace.Level{
		lifecycleKey:          workspace.LevelOrganization,
		"delivery.both_first": workspace.LevelOrganization,
		"delivery.both_last":  workspace.LevelOrganization,
	}, got, "without a workspace, only the organization's locks")
}
