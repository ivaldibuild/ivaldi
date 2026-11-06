package workspaces

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

// world is a Store with the WorkspaceService over it. Tests that use it fix
// the clock, so they must not run in parallel.
type world struct {
	*workspacetest.Store
	authz *workspacetest.Authorizer
	svc   workspace.WorkspaceService
}

func allOptions(s *workspacetest.Store, authz *workspacetest.Authorizer, deps ...workspace.Dependent) []WorkspaceServiceOption {
	return []WorkspaceServiceOption{
		WithRepository(s), WithResourceRegistrar(s), WithDependents(deps...), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(s.Outbox()), WithAuditRecorder(s.Audit()),
	}
}

func newWorld(t *testing.T, deps ...workspace.Dependent) *world {
	t.Helper()
	clock.Fix(t, fixedNow)
	s := workspacetest.NewStore()
	authz := &workspacetest.Authorizer{}
	svc, err := NewWorkspaceService(allOptions(s, authz, deps...)...)
	require.NoError(t, err)
	return &world{Store: s, authz: authz, svc: svc}
}

// defaultWorkspace provisions the Organization's default Workspace, as IAM does.
func (w *world) defaultWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	p, err := workspace.NewProvisioner(w.Store, w.Store, w.Store, w.Outbox(), w.Audit())
	require.NoError(t, err)
	require.NoError(t, p.ProvisionOrganization(context.Background(), org))
	d, err := w.GetDefaultWorkspace(context.Background(), org)
	require.NoError(t, err)
	return d
}

// workspace creates a Workspace through the service.
func (w *world) workspace(t *testing.T, slug string) *workspace.Workspace {
	t.Helper()
	ws, err := w.svc.CreateWorkspace(context.Background(), alice, workspace.CreateWorkspaceInput{OrganizationID: org, Slug: slug, DisplayName: slug})
	require.NoError(t, err)
	return ws
}

// project stores an active Project in ws directly.
func (w *world) project(t *testing.T, ws *workspace.Workspace, slug string) *workspace.Project {
	t.Helper()
	p, err := workspace.NewProject(workspace.NewProjectProps{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: slug, DisplayName: slug}, nil)
	require.NoError(t, err)
	return w.PutProject(p)
}

// reset forgets what was recorded and asked so far.
func (w *world) reset() {
	w.Reset()
	w.authz.Asked = nil
}
