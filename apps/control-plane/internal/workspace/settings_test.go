package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

const lifecycleKey = "delivery.default_lifecycle"

func lifecycleDefinition() Definition {
	return Definition{
		Key:        lifecycleKey,
		Levels:     []Level{LevelOrganization, LevelWorkspace, LevelProject},
		Default:    json.RawMessage(`"lfc_basic"`),
		Permission: "delivery.settings.manage",
		Validate: func(_ context.Context, _ Scope, v json.RawMessage) error {
			var s string
			if err := json.Unmarshal(v, &s); err != nil || !strings.HasPrefix(s, "lfc_") {
				return errors.New("must be a lifecycle ID")
			}
			return nil
		},
	}
}

func TestDefinitionsRegister(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		ok   bool
	}{
		{"valid", lifecycleDefinition(), true},
		{"key without namespace", Definition{Key: "default_lifecycle", Levels: []Level{LevelWorkspace}, Permission: "p"}, false},
		{"uppercase key", Definition{Key: "Delivery.Default", Levels: []Level{LevelWorkspace}, Permission: "p"}, false},
		{"no levels", Definition{Key: "delivery.x", Permission: "p"}, false},
		{"unknown level", Definition{Key: "delivery.x", Levels: []Level{"team"}, Permission: "p"}, false},
		{"no permission", Definition{Key: "delivery.x", Levels: []Level{LevelWorkspace}}, false},
		{"invalid default", Definition{Key: "delivery.x", Levels: []Level{LevelWorkspace}, Permission: "p", Default: json.RawMessage(`{`)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewDefinitions().Register(tt.def)
			if tt.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalid)
			}
		})
	}

	t.Run("duplicate key", func(t *testing.T) {
		d := NewDefinitions()
		require.NoError(t, d.Register(lifecycleDefinition()))
		require.ErrorIs(t, d.Register(lifecycleDefinition()), ErrInvalid)
	})
}

func TestResolve(t *testing.T) {
	stored := func(scope Scope, v string) *Setting {
		return fakeSetting(SettingSnapshot{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(v), Version: 1})
	}
	org := stored(OrganizationScope(testOrg), `"lfc_org"`)
	ws := stored(WorkspaceScope(testOrg, "wsp_1"), `"lfc_ws"`)
	prj := stored(ProjectScope(testOrg, "wsp_1", "prj_1"), `"lfc_prj"`)
	def := lifecycleDefinition()

	tests := []struct {
		name       string
		stored     []*Setting
		lockedAt   Level
		def        Definition
		wantValue  string
		wantLevel  Level
		wantLocked bool
	}{
		{name: "project wins", stored: []*Setting{org, ws, prj}, def: def, wantValue: `"lfc_prj"`, wantLevel: LevelProject},
		{name: "order does not matter", stored: []*Setting{prj, org, ws}, def: def, wantValue: `"lfc_prj"`, wantLevel: LevelProject},
		{name: "workspace when the project has none", stored: []*Setting{org, ws}, def: def, wantValue: `"lfc_ws"`, wantLevel: LevelWorkspace},
		{name: "organization when nothing below", stored: []*Setting{org}, def: def, wantValue: `"lfc_org"`, wantLevel: LevelOrganization},
		{name: "default when nothing is stored", def: def, wantValue: `"lfc_basic"`},
		{name: "nothing when there is no default", def: Definition{Key: "delivery.x"}},
		{name: "organization lock ignores lower values", stored: []*Setting{org, ws, prj}, lockedAt: LevelOrganization, def: def, wantValue: `"lfc_org"`, wantLevel: LevelOrganization, wantLocked: true},
		{name: "organization lock falls back to the default", stored: []*Setting{ws, prj}, lockedAt: LevelOrganization, def: def, wantValue: `"lfc_basic"`, wantLocked: true},
		{name: "workspace lock ignores project values", stored: []*Setting{org, ws, prj}, lockedAt: LevelWorkspace, def: def, wantValue: `"lfc_ws"`, wantLevel: LevelWorkspace, wantLocked: true},
		{name: "workspace lock without a workspace value uses the organization's", stored: []*Setting{org, prj}, lockedAt: LevelWorkspace, def: def, wantValue: `"lfc_org"`, wantLevel: LevelOrganization, wantLocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolve(tt.def, tt.stored, tt.lockedAt)

			if tt.wantValue == "" {
				assert.Nil(t, got.Value)
			} else {
				assert.JSONEq(t, tt.wantValue, string(got.Value))
			}
			assert.Equal(t, tt.wantLevel, got.Level)
			assert.Equal(t, tt.wantLevel == "", got.FromDefault())
			assert.Equal(t, tt.wantLocked, got.Locked)
		})
	}
}

// settingsFixture has a workspace with a project in it, and the lifecycle setting registered.
func settingsFixture(t *testing.T) (*fixture, *Workspace, *Project) {
	t.Helper()
	f := newFixture(t)
	require.NoError(t, f.definitions.Register(lifecycleDefinition()))
	require.NoError(t, f.definitions.Register(Definition{Key: "delivery.workspace_only", Levels: []Level{LevelWorkspace}, Permission: "p"}))
	w := f.workspace(t, "platform")
	return f, w, f.project(t, w, "checkout")
}

func TestChangeSettingIsRefused(t *testing.T) {
	ctx := context.Background()
	f, w, p := settingsFixture(t)
	other := f.workspace(t, "data")

	tests := []struct {
		name    string
		in      ChangeSettingInput
		wantErr error
	}{
		{"unknown key", ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: "delivery.nope", Value: json.RawMessage(`1`)}, ErrUnknownSetting},
		{"level not allowed", ChangeSettingInput{Scope: ProjectScope(testOrg, w.ID(), p.ID()), Key: "delivery.workspace_only", Value: json.RawMessage(`1`)}, ErrLevelNotAllowed},
		{"invalid JSON", ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`{`)}, ErrInvalid},
		{"too large", ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_` + strings.Repeat("x", MaxSettingValueBytes) + `"`)}, ErrInvalid},
		{"rejected by the owning capability", ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`"env_1"`)}, ErrInvalid},
		{"project without its workspace", ChangeSettingInput{Scope: Scope{OrganizationID: testOrg, ProjectID: p.ID()}, Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, ErrInvalid},
		{"project named under another workspace", ChangeSettingInput{Scope: ProjectScope(testOrg, other.ID(), p.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, ErrNotFound},
		{"workspace that does not exist", ChangeSettingInput{Scope: WorkspaceScope(testOrg, "wsp_gone"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)}, ErrNotFound},
		{"stale version", ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`), ExpectedVersion: 3}, ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.svc.ChangeSetting(ctx, alice, tt.in)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("archived workspace", func(t *testing.T) {
		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, other.ID())
		require.NoError(t, err)
		_, err = f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: WorkspaceScope(testOrg, other.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("denied by policy, for example a lock", func(t *testing.T) {
		f.authz.decide = func(authorization.Request) authorization.Decision {
			return authorization.Decision{ID: "dec_lock", Outcome: authorization.OutcomeDeny, Reason: "locked by the organization"}
		}
		defer func() { f.authz.decide = nil }()
		_, err := f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_1"`)})
		require.ErrorIs(t, err, authorization.ErrDenied)
		assert.Contains(t, err.Error(), "locked by the organization")
	})
}

func TestChangeSetting(t *testing.T) {
	ctx := context.Background()
	f, w, _ := settingsFixture(t)
	scope := WorkspaceScope(testOrg, w.ID())

	first, err := f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), first.Version())
	assert.Equal(t, "dec_allow", first.PolicyDecisionID())

	req := f.authz.last()
	assert.Equal(t, "delivery.settings.manage", req.Permission, "the definition's permission is checked")
	assert.Equal(t, w.ID(), req.ResourceID)
	assert.Equal(t, LevelWorkspace, req.Proposal.(SettingChange).Level)

	second, err := f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_hotfix"`), ExpectedVersion: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), second.Version())

	e := f.audit.last()
	assert.Equal(t, EventSettingChanged, e.Type)
	assert.Equal(t, w.ID(), e.ResourceID)
	assert.JSONEq(t, `"lfc_std"`, string(e.Before.(settingAudit).Value))
	assert.JSONEq(t, `"lfc_hotfix"`, string(e.After.(settingAudit).Value))

	t.Run("a change against an old version is refused", func(t *testing.T) {
		_, err := f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
		require.ErrorIs(t, err, ErrConflict)
	})
}

func TestAgentSettingChangesNeedApproval(t *testing.T) {
	ctx := context.Background()
	f, w, _ := settingsFixture(t)
	in := ChangeSettingInput{Scope: WorkspaceScope(testOrg, w.ID()), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`)}

	// Policy as ADR 0007 requires: an agent's change waits for approval, and
	// is allowed once resubmitted with an approved decision.
	f.authz.decide = func(req authorization.Request) authorization.Decision {
		if req.Actor.IsAgent() && req.ApprovalID != "dec_pending" {
			return authorization.Decision{ID: "dec_pending", Outcome: authorization.OutcomeRequiresApproval}
		}
		return authorization.Decision{ID: "dec_pending", Outcome: authorization.OutcomeAllow, ApprovedByPrincipalID: "prn_alice"}
	}

	_, err := f.svc.ChangeSetting(ctx, agent, in)
	var approval *authorization.ApprovalRequiredError
	require.ErrorAs(t, err, &approval)
	assert.Equal(t, "dec_pending", approval.DecisionID)
	_, getErr := f.repo.GetSetting(ctx, in.Scope, lifecycleKey)
	require.ErrorIs(t, getErr, ErrNotFound, "nothing is written while the change waits")

	in.ApprovalID = approval.DecisionID
	s, err := f.svc.ChangeSetting(ctx, agent, in)
	require.NoError(t, err)
	assert.Equal(t, "dec_pending", s.PolicyDecisionID())
	assert.Equal(t, "prn_alice", f.audit.last().ApprovedByPrincipalID)

	t.Run("an allow without a human approver is refused anyway", func(t *testing.T) {
		f.authz.decide = func(authorization.Request) authorization.Decision {
			return authorization.Decision{ID: "dec_bad", Outcome: authorization.OutcomeAllow}
		}
		_, err := f.svc.ChangeSetting(ctx, agent, ChangeSettingInput{Scope: in.Scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
		require.ErrorIs(t, err, authorization.ErrDenied)

		err = f.svc.RemoveSetting(ctx, agent, RemoveSettingInput{Scope: in.Scope, Key: lifecycleKey, ExpectedVersion: 1})
		require.ErrorIs(t, err, authorization.ErrDenied, "removing a setting needs approval too")
	})
}

func TestRemoveSetting(t *testing.T) {
	ctx := context.Background()
	f, w, _ := settingsFixture(t)
	scope := WorkspaceScope(testOrg, w.ID())

	err := f.svc.RemoveSetting(ctx, alice, RemoveSettingInput{Scope: scope, Key: lifecycleKey})
	require.ErrorIs(t, err, ErrNotFound)

	_, err = f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`)})
	require.NoError(t, err)

	require.NoError(t, f.svc.RemoveSetting(ctx, alice, RemoveSettingInput{Scope: scope, Key: lifecycleKey, ExpectedVersion: 1}))
	e := f.audit.last()
	assert.Equal(t, EventSettingRemoved, e.Type)
	assert.JSONEq(t, `"lfc_std"`, string(e.Before.(settingAudit).Value))
	assert.Equal(t, "setting.remove", f.authz.last().Action)

	got, err := f.svc.ResolveSetting(ctx, scope, lifecycleKey)
	require.NoError(t, err)
	assert.True(t, got.FromDefault(), "the value from the next level up applies again")
}

func TestResolveSetting(t *testing.T) {
	ctx := context.Background()
	f, w, p := settingsFixture(t)
	set := func(scope Scope, v string) {
		t.Helper()
		_, err := f.svc.ChangeSetting(ctx, alice, ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(v)})
		require.NoError(t, err)
	}
	set(OrganizationScope(testOrg), `"lfc_org"`)
	set(WorkspaceScope(testOrg, w.ID()), `"lfc_ws"`)
	set(ProjectScope(testOrg, w.ID(), p.ID()), `"lfc_prj"`)
	projectScope := ProjectScope(testOrg, w.ID(), p.ID())

	got, err := f.svc.ResolveSetting(ctx, projectScope, lifecycleKey)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_prj"`, string(got.Value))
	assert.Equal(t, LevelProject, got.Level)

	f.locks[testOrg+"/"+w.ID()] = map[string]Level{lifecycleKey: LevelWorkspace}
	got, err = f.svc.ResolveSetting(ctx, projectScope, lifecycleKey)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_ws"`, string(got.Value), "a workspace lock ignores project values")
	assert.True(t, got.Locked)

	f.locks[testOrg+"/"] = map[string]Level{lifecycleKey: LevelOrganization}
	got, err = f.svc.ResolveSetting(ctx, projectScope, lifecycleKey)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_org"`, string(got.Value), "an organization lock wins over a workspace lock")

	_, err = f.svc.ResolveSetting(ctx, projectScope, "delivery.nope")
	require.ErrorIs(t, err, ErrUnknownSetting)
}

func TestNewSetting(t *testing.T) {
	fixClock(t, t0)
	scope := WorkspaceScope(testOrg, "wsp_1")

	s, err := NewSetting(NewSettingProps{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`), UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1"})

	require.NoError(t, err)
	assert.Equal(t, scope, s.Scope())
	assert.Equal(t, lifecycleKey, s.Key())
	assert.JSONEq(t, `"lfc_std"`, string(s.Value()))
	assert.Equal(t, int64(1), s.Version(), "a new setting starts at version 1")
	assert.Equal(t, "prn_alice", s.UpdatedByPrincipalID())
	assert.Equal(t, "dec_1", s.PolicyDecisionID())
	assert.Equal(t, t0, s.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, t0, s.UpdatedAt())
}

func TestNewSettingIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		key   string
		value string
	}{
		{"no organization", Scope{WorkspaceID: "wsp_1"}, lifecycleKey, `1`},
		{"project without its workspace", Scope{OrganizationID: testOrg, ProjectID: "prj_1"}, lifecycleKey, `1`},
		{"key without a namespace", WorkspaceScope(testOrg, "wsp_1"), "default_lifecycle", `1`},
		{"invalid JSON", WorkspaceScope(testOrg, "wsp_1"), lifecycleKey, `{`},
		{"too large", WorkspaceScope(testOrg, "wsp_1"), lifecycleKey, `"` + strings.Repeat("x", MaxSettingValueBytes) + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSetting(NewSettingProps{Scope: tt.scope, Key: tt.key, Value: json.RawMessage(tt.value), UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1"})
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestSettingChange(t *testing.T) {
	fixClock(t, t0)
	s, err := NewSetting(NewSettingProps{Scope: WorkspaceScope(testOrg, "wsp_1"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`), UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1"})
	require.NoError(t, err)
	fixClock(t, t1)

	require.NoError(t, s.Change(json.RawMessage(`"lfc_hotfix"`), "prn_bob", "dec_2"))

	assert.JSONEq(t, `"lfc_hotfix"`, string(s.Value()))
	assert.Equal(t, int64(2), s.Version(), "each change moves to the next version")
	assert.Equal(t, "prn_bob", s.UpdatedByPrincipalID())
	assert.Equal(t, "dec_2", s.PolicyDecisionID())
	assert.Equal(t, t0, s.CreatedAt())
	assert.Equal(t, t1, s.UpdatedAt())

	t.Run("an invalid change leaves the setting as it was", func(t *testing.T) {
		before := s.Snapshot()
		require.ErrorIs(t, s.Change(json.RawMessage(`{`), "prn_eve", "dec_3"), ErrInvalid)
		assert.Equal(t, before, s.Snapshot())
	})
}

func TestSettingValueCannotBeChangedFromOutside(t *testing.T) {
	value := json.RawMessage(`"lfc_std"`)
	s, err := NewSetting(NewSettingProps{Scope: WorkspaceScope(testOrg, "wsp_1"), Key: lifecycleKey, Value: value, UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1"})
	require.NoError(t, err)

	value[1] = 'X'
	got := s.Value()
	got[1] = 'Y'
	d := s.Snapshot()
	d.Value[1] = 'Z'

	assert.JSONEq(t, `"lfc_std"`, string(s.Value()), "the value given, returned, and in Snapshot are all copies")
}

func TestNewSettingRebuildsAStoredSetting(t *testing.T) {
	d := SettingSnapshot{
		Scope: ProjectScope(testOrg, "wsp_1", "prj_1"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`), Version: 7,
		UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1", CreatedAt: t0, UpdatedAt: t1,
	}

	s, err := NewSetting(NewSettingProps{
		Scope: d.Scope, Key: d.Key, Value: d.Value, UpdatedByPrincipalID: d.UpdatedByPrincipalID,
		PolicyDecisionID: d.PolicyDecisionID, Version: d.Version, CreatedAt: &d.CreatedAt, UpdatedAt: &d.UpdatedAt,
	})

	require.NoError(t, err)
	assert.Equal(t, d, s.Snapshot(), "stored state comes back as it was, including its version")

	_, err = NewSetting(NewSettingProps{Scope: d.Scope, Key: d.Key, Value: d.Value, Version: -1})
	assert.ErrorIs(t, err, ErrInvalid, "a stored version cannot be negative")
}
