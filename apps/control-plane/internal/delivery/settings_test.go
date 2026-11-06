package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

const settingsOrg = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"

// settingLifecycles holds Lifecycles by ID, or fails with err.
type settingLifecycles struct {
	byID map[string]*Lifecycle
	err  error
}

func (s settingLifecycles) GetLifecycle(_ context.Context, organizationID, lifecycleID string) (*Lifecycle, error) {
	if s.err != nil {
		return nil, s.err
	}
	l, ok := s.byID[lifecycleID]
	if !ok || l.OrganizationID() != organizationID {
		return nil, ErrNotFound
	}
	return l, nil
}

func newSettingLifecycle(t *testing.T, workspaceID, slug string, archived bool) *Lifecycle {
	t.Helper()
	l, err := NewLifecycle(NewLifecycleProps{OrganizationID: settingsOrg, WorkspaceID: workspaceID, Slug: slug, DisplayName: "Lifecycle " + slug}, nil)
	require.NoError(t, err)
	if archived {
		l.Archive()
	}
	return l
}

func TestRegisterSettings(t *testing.T) {
	std := newSettingLifecycle(t, "wsp_a", "standard", false)
	old := newSettingLifecycle(t, "wsp_a", "old", true)
	elsewhere := newSettingLifecycle(t, "wsp_b", "elsewhere", false)
	lifecycles := settingLifecycles{byID: map[string]*Lifecycle{std.ID(): std, old.ID(): old, elsewhere.ID(): elsewhere}}

	defs := workspace.NewDefinitions()
	require.NoError(t, RegisterSettings(defs, lifecycles))

	def, ok := defs.Lookup(SettingDefaultLifecycle)
	require.True(t, ok)
	assert.Equal(t, []workspace.Level{workspace.LevelWorkspace, workspace.LevelProject}, def.Levels, "lifecycles belong to a workspace")
	assert.Equal(t, PermissionLifecycleManage, def.Permission)
	assert.Nil(t, def.Default, "there is no default lifecycle")

	_, err := defs.ForScope(workspace.OrganizationScope(settingsOrg), SettingDefaultLifecycle)
	assert.ErrorIs(t, err, workspace.ErrLevelNotAllowed)

	ctx := context.Background()
	wsScope := workspace.WorkspaceScope(settingsOrg, "wsp_a")
	prjScope := workspace.ProjectScope(settingsOrg, "wsp_a", "prj_1")
	value := func(s string) json.RawMessage { return json.RawMessage(`"` + s + `"`) }

	assert.NoError(t, def.ValidateValue(ctx, wsScope, value(std.ID())))
	assert.NoError(t, def.ValidateValue(ctx, prjScope, value(std.ID())))

	for name, c := range map[string]struct {
		scope workspace.Scope
		value json.RawMessage
		want  string
	}{
		"not a string":             {wsScope, json.RawMessage(`42`), "JSON string"},
		"null":                     {wsScope, json.RawMessage(`null`), "not a lifecycle ID"},
		"another type's ID":        {wsScope, value("env_01ARZ3NDEKTSV4RRFFQ69G5FAV"), "not a lifecycle ID"},
		"a lifecycle that is gone": {wsScope, value("lfc_01ARZ3NDEKTSV4RRFFQ69G5FAV"), "does not exist"},
		"an archived lifecycle":    {wsScope, value(old.ID()), "archived"},
		"another workspace's":      {prjScope, value(elsewhere.ID()), "not in workspace wsp_a"},
		"another organization's":   {workspace.WorkspaceScope("org_other", "wsp_a"), value(std.ID()), "does not exist"},
	} {
		t.Run(name, func(t *testing.T) {
			err := def.ValidateValue(ctx, c.scope, c.value)
			require.ErrorIs(t, err, workspace.ErrInvalid)
			assert.Contains(t, err.Error(), c.want)
		})
	}

	t.Run("a failed lookup is not hidden", func(t *testing.T) {
		boom := errors.New("connection reset")
		failing := workspace.NewDefinitions()
		require.NoError(t, RegisterSettings(failing, settingLifecycles{err: boom}))
		def, _ := failing.Lookup(SettingDefaultLifecycle)
		assert.ErrorIs(t, def.ValidateValue(ctx, wsScope, value(std.ID())), boom)
	})

	t.Run("registering twice is refused", func(t *testing.T) {
		assert.ErrorIs(t, RegisterSettings(defs, lifecycles), workspace.ErrInvalid)
	})

	t.Run("needs its dependencies", func(t *testing.T) {
		assert.Error(t, RegisterSettings(nil, lifecycles))
		assert.Error(t, RegisterSettings(workspace.NewDefinitions(), nil))
	})
}

func TestDefaultLifecycleID(t *testing.T) {
	got, err := DefaultLifecycleID(workspace.Resolved{Key: SettingDefaultLifecycle})
	require.NoError(t, err)
	assert.Empty(t, got, "no value names no lifecycle")

	got, err = DefaultLifecycleID(workspace.Resolved{Key: SettingDefaultLifecycle, Value: json.RawMessage(`null`)})
	require.NoError(t, err)
	assert.Empty(t, got)

	id := "lfc_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	got, err = DefaultLifecycleID(workspace.Resolved{Key: SettingDefaultLifecycle, Value: json.RawMessage(`"` + id + `"`)})
	require.NoError(t, err)
	assert.Equal(t, id, got)

	_, err = DefaultLifecycleID(workspace.Resolved{Key: SettingDefaultLifecycle, Value: json.RawMessage(`"nope"`)})
	assert.Error(t, err)
}
