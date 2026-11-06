package workspace

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// eventCase is a change to a domain object and the event it should record,
// built from the object after the change. A nil want means the change
// should record nothing.
type eventCase[T any] struct {
	name   string
	change func(t *testing.T, x T)
	want   func(x T) aggregate.Event
}

type eventRecorder interface {
	PullEvents() []aggregate.Event
}

// checkEvents runs each case on a fresh object whose creation event has
// already been pulled.
func checkEvents[T eventRecorder](t *testing.T, build func(*testing.T) T, cases []eventCase[T]) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := build(t)
			x.PullEvents()

			c.change(t, x)

			got := x.PullEvents()
			if c.want == nil {
				assert.Empty(t, got, "nothing changed")
				return
			}
			assert.Equal(t, []aggregate.Event{c.want(x)}, got)
		})
	}
}

func newTestSetting(t *testing.T) *Setting {
	t.Helper()
	s, err := NewSetting(NewSettingProps{
		Scope: WorkspaceScope(testOrg, "wsp_1"), Key: lifecycleKey, Value: json.RawMessage(`"lfc_std"`),
		UpdatedByPrincipalID: "prn_alice", PolicyDecisionID: "dec_1",
	})
	require.NoError(t, err)
	return s
}

func TestNewRecordsCreatedOnlyForNewObjects(t *testing.T) {
	t.Run("workspace", func(t *testing.T) {
		w := newTestWorkspace(t, "platform")
		assert.Equal(t, []aggregate.Event{WorkspaceCreated{w.Snapshot()}}, w.PullEvents())

		d, err := NewDefaultWorkspace(testOrg)
		require.NoError(t, err)
		assert.Equal(t, []aggregate.Event{WorkspaceCreated{d.Snapshot()}}, d.PullEvents(), "the default one too, created as the default")

		stored, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform"}, new(id.New("wsp")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("project", func(t *testing.T) {
		p := newTestProject(t)
		assert.Equal(t, []aggregate.Event{ProjectCreated{p.Snapshot()}}, p.PullEvents())
		stored, err := NewProject(validProjectProps(), new(id.New("prj")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("setting", func(t *testing.T) {
		s := newTestSetting(t)
		assert.Equal(t, []aggregate.Event{SettingCreated{s.Snapshot()}}, s.PullEvents())
		stored := rebuildSetting(s.Snapshot())
		assert.Empty(t, stored.PullEvents(), "a setting with a creation time is stored")
	})
}

func TestWorkspaceEvents(t *testing.T) {
	build := func(t *testing.T) *Workspace { return newTestWorkspace(t, "platform") }
	checkEvents(t, build, []eventCase[*Workspace]{
		{"update", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.Update(UpdateWorkspaceProps{Description: new("Core services")}))
		}, func(w *Workspace) aggregate.Event { return WorkspaceUpdated{w.Snapshot()} }},
		{"update with the same values", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.Update(UpdateWorkspaceProps{Slug: new("platform"), DisplayName: new(w.DisplayName())}))
		}, nil},
		{"make default", func(t *testing.T, w *Workspace) { require.NoError(t, w.MakeDefault()) },
			func(w *Workspace) aggregate.Event { return WorkspaceMadeDefault{w.Snapshot()} }},
		{"make default twice", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.MakeDefault())
			w.PullEvents()
			require.NoError(t, w.MakeDefault())
		}, nil},
		{"clear default", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.MakeDefault())
			w.PullEvents()
			w.ClearDefault()
		}, func(w *Workspace) aggregate.Event { return WorkspaceDefaultCleared{w.Snapshot()} }},
		{"clear default when not the default", func(t *testing.T, w *Workspace) { w.ClearDefault() }, nil},
		{"archive", func(t *testing.T, w *Workspace) { require.NoError(t, w.Archive()) },
			func(w *Workspace) aggregate.Event { return WorkspaceArchived{w.Snapshot()} }},
		{"archive twice", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.Archive())
			w.PullEvents()
			require.NoError(t, w.Archive())
		}, nil},
		{"restore", func(t *testing.T, w *Workspace) {
			require.NoError(t, w.Archive())
			w.PullEvents()
			w.Restore()
		}, func(w *Workspace) aggregate.Event { return WorkspaceRestored{w.Snapshot()} }},
		{"restore an active one", func(t *testing.T, w *Workspace) { w.Restore() }, nil},
	})
}

func TestProjectEvents(t *testing.T) {
	checkEvents(t, newTestProject, []eventCase[*Project]{
		{"update", func(t *testing.T, p *Project) {
			require.NoError(t, p.Update(UpdateProjectProps{DisplayName: new("Checkout API")}))
		}, func(p *Project) aggregate.Event { return ProjectUpdated{p.Snapshot()} }},
		{"update with the same values", func(t *testing.T, p *Project) {
			require.NoError(t, p.Update(UpdateProjectProps{Slug: new("checkout"), Description: new("")}))
		}, nil},
		{"transfer", func(t *testing.T, p *Project) { require.NoError(t, p.TransferTo("tem_2")) },
			func(p *Project) aggregate.Event {
				return ProjectTransferred{Project: p.Snapshot(), PreviousOwnerTeamID: testTeam}
			}},
		{"transfer to the same team", func(t *testing.T, p *Project) { require.NoError(t, p.TransferTo(testTeam)) }, nil},
		{"archive", func(t *testing.T, p *Project) { p.Archive() },
			func(p *Project) aggregate.Event { return ProjectArchived{p.Snapshot()} }},
		{"archive twice", func(t *testing.T, p *Project) { p.Archive(); p.PullEvents(); p.Archive() }, nil},
		{"restore", func(t *testing.T, p *Project) { p.Archive(); p.PullEvents(); p.Restore() },
			func(p *Project) aggregate.Event { return ProjectRestored{p.Snapshot()} }},
		{"restore an active one", func(t *testing.T, p *Project) { p.Restore() }, nil},
	})
}

func TestSettingEvents(t *testing.T) {
	checkEvents(t, newTestSetting, []eventCase[*Setting]{
		{"change", func(t *testing.T, s *Setting) {
			require.NoError(t, s.Change(json.RawMessage(`"lfc_fast"`), "prn_bob", "dec_2"))
		}, func(s *Setting) aggregate.Event {
			return SettingChanged{Setting: s.Snapshot(), PreviousValue: json.RawMessage(`"lfc_std"`)}
		}},
		{"change to the same value", func(t *testing.T, s *Setting) {
			require.NoError(t, s.Change(json.RawMessage(`"lfc_std"`), "prn_bob", "dec_2"))
		}, nil},
		{"remove", func(t *testing.T, s *Setting) { s.Remove("prn_bob", "dec_3") },
			func(s *Setting) aggregate.Event {
				return SettingRemoved{Setting: s.Snapshot(), RemovedByPrincipalID: "prn_bob", PolicyDecisionID: "dec_3"}
			}},
	})
}

// The snapshots' JSON keys are the events' contract: a change here is a
// change for every consumer.
func TestSnapshotJSONKeys(t *testing.T) {
	timestamps := []string{"created_at", "updated_at"}
	tests := []struct {
		snapshot any
		keys     []string
	}{
		{WorkspaceSnapshot{}, append([]string{"id", "organization_id", "slug", "display_name", "description", "is_default", "created_by_principal_id", "archived_at"}, timestamps...)},
		{ProjectSnapshot{}, append([]string{"id", "organization_id", "workspace_id", "owner_team_id", "slug", "display_name", "description", "created_by_principal_id", "archived_at"}, timestamps...)},
		{SettingSnapshot{Value: json.RawMessage(`null`)}, append([]string{"scope", "key", "value", "version", "updated_by_principal_id", "policy_decision_id"}, timestamps...)},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.snapshot)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &fields))

		assert.ElementsMatch(t, tt.keys, slices.Collect(maps.Keys(fields)), "%T", tt.snapshot)
	}
}

func TestScopeJSON(t *testing.T) {
	b, err := json.Marshal(ProjectScope(testOrg, "wsp_1", "prj_1"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"organization_id": "`+testOrg+`", "workspace_id": "wsp_1", "project_id": "prj_1"}`, string(b))

	b, err = json.Marshal(OrganizationScope(testOrg))
	require.NoError(t, err)
	assert.JSONEq(t, `{"organization_id": "`+testOrg+`"}`, string(b), "an organization scope names no workspace or project")
}
