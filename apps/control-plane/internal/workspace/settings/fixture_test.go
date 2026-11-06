package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	org          = workspacetest.Organization
	lifecycleKey = "delivery.default_lifecycle"
	// workspaceOnlyKey may be set only at Workspace level.
	workspaceOnlyKey = "delivery.workspace_only"
)

var (
	alice    = workspacetest.Caller
	agent    = identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorAgentSession, SessionID: "ses_1"}
	fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
)

func lifecycleDefinition() workspace.Definition {
	return workspace.Definition{
		Key:        lifecycleKey,
		Levels:     []workspace.Level{workspace.LevelOrganization, workspace.LevelWorkspace, workspace.LevelProject},
		Default:    json.RawMessage(`"lfc_basic"`),
		Permission: "delivery.settings.manage",
		Validate: func(_ context.Context, _ workspace.Scope, v json.RawMessage) error {
			var s string
			if err := json.Unmarshal(v, &s); err != nil || !strings.HasPrefix(s, "lfc_") {
				return fmt.Errorf("%w: must be a lifecycle ID", workspace.ErrInvalid)
			}
			return nil
		},
	}
}

// world is a Store with a Workspace and a Project in it, the lifecycle
// setting registered, and the SettingService over it. Tests that use it fix
// the clock, so they must not run in parallel.
type world struct {
	*workspacetest.Store
	authz       *workspacetest.Authorizer
	definitions *workspace.Definitions
	svc         workspace.SettingService
	ws          *workspace.Workspace
	project     *workspace.Project
}

func allOptions(s *workspacetest.Store, definitions *workspace.Definitions, authz *workspacetest.Authorizer) []SettingServiceOption {
	return []SettingServiceOption{
		WithRepository(s), WithDefinitions(definitions), WithLockSource(s), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(s.Outbox()), WithAuditRecorder(s.Audit()),
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, fixedNow)
	s := workspacetest.NewStore()
	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(lifecycleDefinition()))
	require.NoError(t, defs.Register(workspace.Definition{Key: workspaceOnlyKey, Levels: []workspace.Level{workspace.LevelWorkspace}, Permission: "p"}))
	authz := &workspacetest.Authorizer{}
	svc, err := NewSettingService(allOptions(s, defs, authz)...)
	require.NoError(t, err)
	w := &world{Store: s, authz: authz, definitions: defs, svc: svc}
	w.ws = w.workspace(t, "platform", false)
	p, err := workspace.NewProject(workspace.NewProjectProps{OrganizationID: org, WorkspaceID: w.ws.ID(), OwnerTeamID: "tem_1", Slug: "checkout", DisplayName: "Checkout"}, nil)
	require.NoError(t, err)
	w.project = s.PutProject(p)
	return w
}

// workspace stores a Workspace directly.
func (w *world) workspace(t *testing.T, slug string, archived bool) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: slug, DisplayName: slug}, nil)
	require.NoError(t, err)
	if archived {
		require.NoError(t, ws.Archive())
	}
	return w.Put(ws)
}

func (w *world) workspaceScope() workspace.Scope { return workspace.WorkspaceScope(org, w.ws.ID()) }

func (w *world) projectScope() workspace.Scope {
	return workspace.ProjectScope(org, w.ws.ID(), w.project.ID())
}

// set changes a setting through the service as Alice.
func (w *world) set(t *testing.T, scope workspace.Scope, value string, expectedVersion int64) *workspace.Setting {
	t.Helper()
	s, err := w.svc.ChangeSetting(context.Background(), alice, workspace.ChangeSettingInput{Scope: scope, Key: lifecycleKey, Value: json.RawMessage(value), ExpectedVersion: expectedVersion})
	require.NoError(t, err)
	return s
}

// reset forgets what was recorded and asked so far.
func (w *world) reset() {
	w.Reset()
	w.authz.Asked = nil
}
