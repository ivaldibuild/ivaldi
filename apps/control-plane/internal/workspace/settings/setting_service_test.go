package settings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

func TestNewSettingServiceRequiresEveryDependency(t *testing.T) {
	s := workspacetest.NewStore()
	all := allOptions(s, workspace.NewDefinitions(), &workspacetest.Authorizer{})
	for i := range all {
		opts := append(append([]SettingServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewSettingService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewSettingService(all...)
	assert.NoError(t, err)
}

func TestTheServiceRefusesAChangeThatWasNotAuthorized(t *testing.T) {
	w := newWorld(t)
	r, err := workspace.NewSettingResolver(w.Store, w.definitions, w.Store)
	require.NoError(t, err)
	svc := &service{repo: w.Store, definitions: w.definitions, locks: w.Store, resolver: r, w: change.Writer{Tx: w.Store, Outbox: w.Outbox(), Audit: w.Audit()}}

	_, err = svc.ChangeSetting(context.Background(), alice, workspace.ChangeSettingInput{Scope: w.workspaceScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`)})

	require.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Empty(t, w.Settings, "nothing is written without a decision")
}

// Every operation asks the authorizer the same question the workspace
// capability has always asked, about the level's resource.
func TestOperationsAreAuthorizedOnTheirLevel(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	stored := w.set(t, w.projectScope(), `"lfc_std"`, 0)
	w.authz.Asked = nil

	change := workspace.ChangeSettingInput{Scope: w.workspaceScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_fast"`)}
	remove := workspace.RemoveSettingInput{Scope: w.projectScope(), Key: lifecycleKey, ExpectedVersion: 1, ApprovalID: "dec_approved"}

	cases := []struct {
		name string
		call func() error
		want authorization.Request
	}{
		{"change", func() error { _, err := w.svc.ChangeSetting(ctx, alice, change); return err }, authorization.Request{
			Actor: alice, Permission: "delivery.settings.manage", OrganizationID: org, ResourceID: w.ws.ID(), Action: workspace.ActionSettingChange,
			Proposal: workspace.SettingChange{Scope: change.Scope, Key: lifecycleKey, Level: workspace.LevelWorkspace, Value: change.Value},
		}},
		{"list", func() error { _, err := w.svc.ListSettings(ctx, alice, w.projectScope()); return err }, authorization.Request{
			Actor: alice, Permission: workspace.PermissionSettingRead, OrganizationID: org, ResourceID: w.project.ID(), Action: workspace.ActionSettingList,
		}},
		{"resolve", func() error {
			_, err := w.svc.ResolveSetting(ctx, alice, workspace.OrganizationScope(org), lifecycleKey)
			return err
		}, authorization.Request{
			Actor: alice, Permission: workspace.PermissionSettingRead, OrganizationID: org, ResourceID: org, Action: workspace.ActionSettingRead,
		}},
		{"remove", func() error { return w.svc.RemoveSetting(ctx, alice, remove) }, authorization.Request{
			Actor: alice, Permission: "delivery.settings.manage", OrganizationID: org, ResourceID: w.project.ID(), Action: workspace.ActionSettingRemove,
			Proposal:   workspace.SettingChange{Scope: remove.Scope, Key: lifecycleKey, Level: workspace.LevelProject, Before: stored.Value(), ExpectedVersion: 1},
			ApprovalID: "dec_approved",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.authz.Asked = nil
			require.NoError(t, tc.call())
			require.Len(t, w.authz.Asked, 1)
			assert.Equal(t, tc.want, w.authz.Asked[0])
		})
	}
}

func TestChangeSettingIsRefused(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	other := w.workspace(t, "data", false)
	archived := w.workspace(t, "old", true)
	wsScope, prjScope := w.workspaceScope(), w.projectScope()

	tests := []struct {
		name         string
		in           workspace.ChangeSettingInput
		wantErr      error
		wantNotAsked bool
	}{
		{"unknown key", workspace.ChangeSettingInput{Scope: wsScope, Key: "delivery.nope", Value: json.RawMessage(`1`)}, workspace.ErrUnknownSetting, true},
		{"level not allowed", workspace.ChangeSettingInput{Scope: prjScope, Key: workspaceOnlyKey, Value: json.RawMessage(`1`)}, workspace.ErrLevelNotAllowed, true},
		{"invalid JSON", workspace.ChangeSettingInput{Scope: wsScope, Key: lifecycleKey, Value: json.RawMessage(`{`)}, workspace.ErrInvalid, true},
		{"too large", workspace.ChangeSettingInput{Scope: wsScope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_` + strings.Repeat("x", workspace.MaxSettingValueBytes) + `"`)}, workspace.ErrInvalid, true},
		{"project without its workspace", workspace.ChangeSettingInput{Scope: workspace.Scope{OrganizationID: org, ProjectID: w.project.ID()}, Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, workspace.ErrInvalid, true},
		{"stale version", workspace.ChangeSettingInput{Scope: wsScope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`), ExpectedVersion: 3}, workspace.ErrConflict, true},
		{"rejected by the owning capability", workspace.ChangeSettingInput{Scope: wsScope, Key: lifecycleKey, Value: json.RawMessage(`"env_1"`)}, workspace.ErrInvalid, false},
		{"project named under another workspace", workspace.ChangeSettingInput{Scope: workspace.ProjectScope(org, other.ID(), w.project.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, workspace.ErrNotFound, false},
		{"workspace that does not exist", workspace.ChangeSettingInput{Scope: workspace.WorkspaceScope(org, "wsp_gone"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, workspace.ErrNotFound, false},
		{"archived workspace", workspace.ChangeSettingInput{Scope: workspace.WorkspaceScope(org, archived.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, workspace.ErrArchived, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.reset()
			_, err := w.svc.ChangeSetting(ctx, alice, tt.in)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, w.Settings)
			assert.Empty(t, w.AuditLog)
			assert.Empty(t, w.Events)
			if tt.wantNotAsked {
				assert.Empty(t, w.authz.Asked, "refused before asking")
			}
		})
	}

	t.Run("denied by policy, for example a lock", func(t *testing.T) {
		w.authz.Decide = func(authorization.Request) authorization.Decision {
			return authorization.Decision{ID: "dec_lock", Outcome: authorization.OutcomeDeny, Reason: "locked by the organization"}
		}
		defer func() { w.authz.Decide = nil }()
		_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: wsScope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, authorization.ErrDenied)
		assert.Contains(t, err.Error(), "locked by the organization")
	})
}

func TestChangeSetting(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	scope := w.workspaceScope()

	first := w.set(t, scope, `"lfc_std"`, 0)
	assert.Equal(t, int64(1), first.Version())
	assert.Equal(t, "dec-"+w.ws.ID(), first.PolicyDecisionID(), "the setting records the decision that allowed it")
	assert.Equal(t, alice.PrincipalID, first.UpdatedByPrincipalID())
	assert.Equal(t, []string{"workspace.v1.setting.created"}, w.EventTypes())
	assert.Equal(t, workspace.AggregateSetting, w.Events[0].AggregateType)
	assert.Equal(t, w.ws.ID()+":"+lifecycleKey, w.Events[0].AggregateID)

	req := w.authz.Last()
	assert.Equal(t, "delivery.settings.manage", req.Permission, "the definition's permission is checked")
	assert.Equal(t, w.ws.ID(), req.ResourceID)
	assert.Equal(t, workspace.LevelWorkspace, req.Proposal.(workspace.SettingChange).Level)
	w.reset()

	second := w.set(t, scope, `"lfc_hotfix"`, 1)
	assert.Equal(t, int64(2), second.Version())
	assert.JSONEq(t, `"lfc_std"`, string(w.authz.Last().Proposal.(workspace.SettingChange).Before), "the proposal shows the value it replaces")
	assert.Equal(t, []string{"workspace.v1.setting.changed"}, w.EventTypes())

	e := w.LastAudit()
	assert.Equal(t, workspace.EventSettingChanged, e.Type)
	assert.Equal(t, w.ws.ID(), e.ResourceID)
	assert.Equal(t, "dec-"+w.ws.ID(), e.DecisionID)
	assert.Equal(t, fixedNow, e.OccurredAt)
	assert.JSONEq(t, `"lfc_std"`, string(e.Before.(settingAudit).Value))
	assert.JSONEq(t, `"lfc_hotfix"`, string(e.After.(settingAudit).Value))

	t.Run("a change against an old version is refused", func(t *testing.T) {
		_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
		require.ErrorIs(t, err, workspace.ErrConflict)
	})
}

func TestAgentSettingChangesNeedApproval(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	in := workspace.ChangeSettingInput{Scope: w.workspaceScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`)}

	// Policy as ADR 0007 requires: an agent's change waits for approval, and
	// is allowed once resubmitted with an approved decision.
	w.authz.Decide = func(req authorization.Request) authorization.Decision {
		if req.Actor.IsAgent() && req.ApprovalID != "dec_pending" {
			return authorization.Decision{ID: "dec_pending", Outcome: authorization.OutcomeRequiresApproval}
		}
		return authorization.Decision{ID: "dec_pending", Outcome: authorization.OutcomeAllow, ApprovedByPrincipalID: "prn_alice"}
	}

	_, err := w.svc.ChangeSetting(ctx, agent, in)
	var approval *authorization.ApprovalRequiredError
	require.ErrorAs(t, err, &approval)
	assert.Equal(t, "dec_pending", approval.DecisionID)
	assert.Empty(t, w.Settings, "nothing is written while the change waits")

	in.ApprovalID = approval.DecisionID
	s, err := w.svc.ChangeSetting(ctx, agent, in)
	require.NoError(t, err)
	assert.Equal(t, "dec_pending", s.PolicyDecisionID())
	assert.Equal(t, "prn_alice", w.LastAudit().ApprovedByPrincipalID)
	assert.Equal(t, "dec_pending", w.LastAudit().DecisionID)

	t.Run("an allow without a human approver is refused anyway", func(t *testing.T) {
		w.authz.Decide = func(authorization.Request) authorization.Decision {
			return authorization.Decision{ID: "dec_bad", Outcome: authorization.OutcomeAllow}
		}
		_, err := w.svc.ChangeSetting(ctx, agent, workspace.ChangeSettingInput{Scope: in.Scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
		require.ErrorIs(t, err, authorization.ErrDenied)

		err = w.svc.RemoveSetting(ctx, agent, workspace.RemoveSettingInput{Scope: in.Scope, Key: lifecycleKey, ExpectedVersion: 1})
		require.ErrorIs(t, err, authorization.ErrDenied, "removing a setting needs approval too")
	})
}

func TestRemoveSetting(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	scope := w.workspaceScope()

	err := w.svc.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: scope, Key: lifecycleKey})
	require.ErrorIs(t, err, workspace.ErrNotFound)
	assert.Empty(t, w.authz.Asked, "removing nothing is refused before asking")

	w.set(t, scope, `"lfc_std"`, 0)
	w.reset()

	err = w.svc.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: scope, Key: lifecycleKey, ExpectedVersion: 2})
	require.ErrorIs(t, err, workspace.ErrConflict)

	require.NoError(t, w.svc.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: scope, Key: lifecycleKey, ExpectedVersion: 1}))
	e := w.LastAudit()
	assert.Equal(t, workspace.EventSettingRemoved, e.Type)
	assert.JSONEq(t, `"lfc_std"`, string(e.Before.(settingAudit).Value))
	assert.Equal(t, "dec-"+w.ws.ID(), e.DecisionID)
	assert.Equal(t, "setting.remove", w.authz.Last().Action)
	assert.Equal(t, []string{"workspace.v1.setting.removed"}, w.EventTypes())
	assert.Empty(t, w.Settings)

	got, err := w.svc.ResolveSetting(ctx, alice, scope, lifecycleKey)
	require.NoError(t, err)
	assert.True(t, got.FromDefault(), "the value from the next level up applies again")
}

func TestResolveSetting(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.set(t, workspace.OrganizationScope(org), `"lfc_org"`, 0)
	w.set(t, w.workspaceScope(), `"lfc_ws"`, 0)
	w.set(t, w.projectScope(), `"lfc_prj"`, 0)

	got, err := w.svc.ResolveSetting(ctx, alice, w.projectScope(), lifecycleKey)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_prj"`, string(got.Value))
	assert.Equal(t, workspace.LevelProject, got.Level)

	w.Locks[org+"/"+w.ws.ID()] = map[string]workspace.Level{lifecycleKey: workspace.LevelWorkspace}
	got, err = w.svc.ResolveSetting(ctx, alice, w.projectScope(), lifecycleKey)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_ws"`, string(got.Value), "a workspace lock ignores project values")
	assert.True(t, got.Locked)

	_, err = w.svc.ResolveSetting(ctx, alice, w.projectScope(), "delivery.nope")
	require.ErrorIs(t, err, workspace.ErrUnknownSetting)

	w.authz.Decide = workspacetest.Deny
	_, err = w.svc.ResolveSetting(ctx, alice, w.projectScope(), lifecycleKey)
	require.ErrorIs(t, err, authorization.ErrDenied)
}

func TestListSettings(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.set(t, workspace.OrganizationScope(org), `"lfc_org"`, 0)
	w.set(t, w.workspaceScope(), `"lfc_ws"`, 0)

	got, err := w.svc.ListSettings(ctx, alice, w.workspaceScope())
	require.NoError(t, err)
	require.Len(t, got, 1, "only the values stored at exactly that level")
	assert.JSONEq(t, `"lfc_ws"`, string(got[0].Value()))

	w.authz.Asked = nil
	_, err = w.svc.ListSettings(ctx, alice, workspace.Scope{OrganizationID: org, ProjectID: w.project.ID()})
	require.ErrorIs(t, err, workspace.ErrInvalid)
	assert.Empty(t, w.authz.Asked)
}

// Locks come from the policy capability through workspace.LockSource; the
// Store's Locks play that part here.
func TestSettingsBelowALockCannotChange(t *testing.T) {
	ctx := context.Background()
	orgScope := workspace.OrganizationScope(org)

	t.Run("an organization lock fixes every workspace and project", func(t *testing.T) {
		w := newWorld(t)
		w.Locks[org+"/"] = map[string]workspace.Level{lifecycleKey: workspace.LevelOrganization}

		for _, scope := range []workspace.Scope{w.workspaceScope(), w.projectScope()} {
			w.reset()
			_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
			require.ErrorIs(t, err, workspace.ErrLocked, "at %s level", scope.Level())
			assert.Contains(t, err.Error(), "locked at organization level")
			assert.Empty(t, w.authz.Asked, "refused before asking, so no approval is requested for a change that cannot apply")
		}
		assert.Empty(t, w.Settings)

		w.set(t, orgScope, `"lfc_org"`, 0)
		assert.Len(t, w.Settings, 1, "the organization's own value can change")
	})

	t.Run("a workspace lock fixes its projects only", func(t *testing.T) {
		w := newWorld(t)
		w.Locks[org+"/"+w.ws.ID()] = map[string]workspace.Level{lifecycleKey: workspace.LevelWorkspace}

		_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: w.projectScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, workspace.ErrLocked)
		assert.Contains(t, err.Error(), "locked at workspace level")

		w.set(t, w.workspaceScope(), `"lfc_ws"`, 0)
		w.set(t, orgScope, `"lfc_org"`, 0)

		other := w.workspace(t, "data", false)
		w.set(t, workspace.WorkspaceScope(org, other.ID()), `"lfc_data"`, 0)
	})

	t.Run("an organization lock wins over a workspace lock", func(t *testing.T) {
		w := newWorld(t)
		w.Locks[org+"/"+w.ws.ID()] = map[string]workspace.Level{lifecycleKey: workspace.LevelWorkspace}
		w.Locks[org+"/"] = map[string]workspace.Level{lifecycleKey: workspace.LevelOrganization}

		_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: w.workspaceScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, workspace.ErrLocked, "the workspace cannot change a key the organization locked")
	})

	t.Run("a value below a lock cannot be removed until the lock is", func(t *testing.T) {
		w := newWorld(t)
		w.set(t, w.projectScope(), `"lfc_prj"`, 0)
		w.Locks[org+"/"+w.ws.ID()] = map[string]workspace.Level{lifecycleKey: workspace.LevelWorkspace}
		w.reset()

		err := w.svc.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: w.projectScope(), Key: lifecycleKey, ExpectedVersion: 1})
		require.ErrorIs(t, err, workspace.ErrLocked)
		assert.Empty(t, w.authz.Asked)
		assert.Len(t, w.Settings, 1, "the value is kept, inactive")

		delete(w.Locks, org+"/"+w.ws.ID())
		require.NoError(t, w.svc.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: w.projectScope(), Key: lifecycleKey, ExpectedVersion: 1}))
	})

	t.Run("a lock added after the decision is still enforced", func(t *testing.T) {
		w := newWorld(t)
		w.authz.Decide = func(req authorization.Request) authorization.Decision {
			w.Locks[org+"/"] = map[string]workspace.Level{lifecycleKey: workspace.LevelOrganization}
			return authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
		}
		_, err := w.svc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: w.workspaceScope(), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, workspace.ErrLocked)
		assert.Empty(t, w.Settings)
		assert.Empty(t, w.AuditLog)
		assert.Empty(t, w.Events)
	})
}

func TestResolvingALockedSetting(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.set(t, w.workspaceScope(), `"lfc_ws"`, 0)
	w.set(t, w.projectScope(), `"lfc_prj"`, 0)

	resolve := func() workspace.Resolved {
		t.Helper()
		got, err := w.svc.ResolveSetting(ctx, alice, w.projectScope(), lifecycleKey)
		require.NoError(t, err)
		return got
	}

	w.Locks[org+"/"] = map[string]workspace.Level{lifecycleKey: workspace.LevelOrganization}
	got := resolve()
	assert.JSONEq(t, `"lfc_basic"`, string(got.Value), "an organization lock without an organization value gives the default")
	assert.True(t, got.FromDefault())
	assert.True(t, got.Locked)

	w.set(t, workspace.OrganizationScope(org), `"lfc_org"`, 0)
	got = resolve()
	assert.JSONEq(t, `"lfc_org"`, string(got.Value))
	assert.Equal(t, workspace.LevelOrganization, got.Level)
	assert.True(t, got.Locked)

	delete(w.Locks, org+"/")
	got = resolve()
	assert.JSONEq(t, `"lfc_prj"`, string(got.Value), "removing the lock brings the values below it back")
	assert.False(t, got.Locked)
}
