package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
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
				return fmt.Errorf("%w: must be a lifecycle ID", ErrInvalid)
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
		return rebuildSetting(SettingSnapshot{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(v), Version: 1})
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

func TestNewSetting(t *testing.T) {
	clock.Fix(t, t0)
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
	clock.Fix(t, t0)
	s, err := NewSetting(NewSettingProps{Scope: WorkspaceScope(testOrg, "wsp_1"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`), UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1"})
	require.NoError(t, err)
	clock.Fix(t, t1)

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

func TestLevelBelow(t *testing.T) {
	assert.True(t, LevelProject.Below(LevelWorkspace))
	assert.True(t, LevelProject.Below(LevelOrganization))
	assert.True(t, LevelWorkspace.Below(LevelOrganization))
	assert.False(t, LevelWorkspace.Below(LevelWorkspace), "a level is not below itself")
	assert.False(t, LevelOrganization.Below(LevelWorkspace))
	assert.False(t, Level("team").Below(LevelOrganization), "an unknown level is below nothing")
	assert.False(t, LevelProject.Below(Level("")))
}

func TestValidateSettingKey(t *testing.T) {
	require.NoError(t, ValidateSettingKey("delivery.default_lifecycle"))
	for _, k := range []string{"", "lifecycle", "Delivery.x", "delivery.", ".x", "delivery.default-lifecycle"} {
		assert.ErrorIs(t, ValidateSettingKey(k), ErrInvalid, "%q", k)
	}
}

func TestValidateValueKeepsUnexpectedErrorsApart(t *testing.T) {
	scope := Scope{OrganizationID: "org_1", WorkspaceID: "wsp_1"}
	def := lifecycleDefinition()

	err := def.ValidateValue(context.Background(), scope, json.RawMessage(`"nope"`))
	require.ErrorIs(t, err, ErrInvalid, "a refused value is invalid input")

	def.Validate = func(context.Context, Scope, json.RawMessage) error {
		return fmt.Errorf("look up lifecycle: connection refused")
	}
	err = def.ValidateValue(context.Background(), scope, json.RawMessage(`"lfc_x"`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalid, "a failed lookup is unexpected, so its message is not shown to the caller")
}
