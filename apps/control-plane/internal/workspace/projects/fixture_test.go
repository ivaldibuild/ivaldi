package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

const (
	org      = workspacetest.Organization
	testTeam = "tem_1"
)

var (
	alice    = workspacetest.Caller
	fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	errBoom  = errors.New("boom")
)

// world is a Store with two active Teams and the ProjectService over it.
// Tests that use it fix the clock, so they must not run in parallel.
type world struct {
	*workspacetest.Store
	authz *workspacetest.Authorizer
	svc   workspace.ProjectService
}

func allOptions(s *workspacetest.Store, authz *workspacetest.Authorizer, deps ...workspace.Dependent) []ProjectServiceOption {
	return []ProjectServiceOption{
		WithRepository(s), WithResourceRegistrar(s), WithTeams(s), WithDependents(deps...), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(s.Outbox()), WithAuditRecorder(s.Audit()),
	}
}

func newWorld(t *testing.T, deps ...workspace.Dependent) *world {
	t.Helper()
	clock.Fix(t, fixedNow)
	s := workspacetest.NewStore()
	s.Teams[org+"/"+testTeam] = true
	s.Teams[org+"/tem_2"] = true
	authz := &workspacetest.Authorizer{}
	svc, err := NewProjectService(allOptions(s, authz, deps...)...)
	require.NoError(t, err)
	return &world{Store: s, authz: authz, svc: svc}
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

// archiveWorkspace archives a stored Workspace directly.
func (w *world) archiveWorkspace(t *testing.T, ws *workspace.Workspace, archived bool) {
	t.Helper()
	got, err := w.GetWorkspace(context.Background(), org, ws.ID())
	require.NoError(t, err)
	if archived {
		require.NoError(t, got.Archive())
	} else {
		got.Restore()
	}
	w.Put(got)
}

// project creates a Project through the service.
func (w *world) project(t *testing.T, ws *workspace.Workspace, slug string) *workspace.Project {
	t.Helper()
	p, err := w.svc.CreateProject(context.Background(), alice, workspace.CreateProjectInput{
		OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: slug, DisplayName: slug,
	})
	require.NoError(t, err)
	return p
}

// reset forgets what was recorded and asked so far.
func (w *world) reset() {
	w.Reset()
	w.authz.Asked = nil
}
