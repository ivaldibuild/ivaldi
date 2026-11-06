package projects

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestNewProjectServiceRequiresEveryDependency(t *testing.T) {
	s := workspacetest.NewStore()
	all := allOptions(s, &workspacetest.Authorizer{})
	for i := range all {
		if i == 3 {
			continue // WithDependents is optional.
		}
		opts := append(append([]ProjectServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewProjectService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewProjectService(all...)
	assert.NoError(t, err)
}

func TestTheServiceRefusesAChangeThatWasNotAuthorized(t *testing.T) {
	w := newWorld(t)
	ws := w.workspace(t, "platform", false)
	svc := &service{repo: w.Store, resources: w.Store, teams: w.Store, w: change.Writer{Tx: w.Store, Outbox: w.Outbox(), Audit: w.Audit()}}

	_, err := svc.CreateProject(context.Background(), alice, workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"})

	require.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Empty(t, w.Projects, "nothing is written without a decision")
}

// Every operation asks the authorizer the same question the workspace
// capability has always asked, about the same resource.
func TestOperationsAreAuthorizedOnTheirResource(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	ws := w.workspace(t, "platform", false)
	p := w.project(t, ws, "checkout")
	archived := w.project(t, ws, "legacy")
	_, err := w.svc.ArchiveProject(ctx, alice, org, archived.ID())
	require.NoError(t, err)
	create := workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: "payments", DisplayName: "Payments"}
	update := workspace.UpdateProjectProps{DisplayName: new("Checkout 2")}

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
		resource   string
		proposal   any
	}{
		{"create", func() error { _, err := w.svc.CreateProject(ctx, alice, create); return err },
			workspace.PermissionProjectManage, workspace.ActionProjectCreate, ws.ID(), create},
		{"get", func() error { _, err := w.svc.GetProject(ctx, alice, org, p.ID()); return err },
			workspace.PermissionProjectRead, workspace.ActionProjectRead, p.ID(), nil},
		{"list", func() error {
			_, err := w.svc.ListProjects(ctx, alice, workspace.ListProjectsQuery{OrganizationID: org, WorkspaceID: ws.ID()})
			return err
		},
			workspace.PermissionProjectRead, workspace.ActionProjectList, ws.ID(), nil},
		{"update", func() error { _, err := w.svc.UpdateProject(ctx, alice, org, p.ID(), update); return err },
			workspace.PermissionProjectManage, workspace.ActionProjectUpdate, p.ID(), update},
		{"transfer", func() error { _, err := w.svc.TransferProject(ctx, alice, org, p.ID(), "tem_2"); return err },
			workspace.PermissionProjectManage, workspace.ActionProjectTransfer, p.ID(), "tem_2"},
		{"archive", func() error { _, err := w.svc.ArchiveProject(ctx, alice, org, archived.ID()); return err },
			workspace.PermissionProjectManage, workspace.ActionProjectArchive, archived.ID(), nil},
		{"restore", func() error { _, err := w.svc.RestoreProject(ctx, alice, org, archived.ID()); return err },
			workspace.PermissionProjectManage, workspace.ActionProjectRestore, ws.ID(), archived.ID()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.authz.Asked = nil
			require.NoError(t, tc.call())
			require.Len(t, w.authz.Asked, 1)
			assert.Equal(t, authorization.Request{
				Actor: alice, Permission: tc.permission, OrganizationID: org,
				ResourceID: tc.resource, Action: tc.action, Proposal: tc.proposal,
			}, w.authz.Asked[0])
		})
	}

	t.Run("a refusal stops the operation", func(t *testing.T) {
		w.authz.Decide = workspacetest.Deny
		defer func() { w.authz.Decide = nil }()
		for _, tc := range cases {
			assert.ErrorIs(t, tc.call(), authorization.ErrDenied, tc.name)
		}
	})
}

func TestCreateProject(t *testing.T) {
	ctx := context.Background()

	refused := []struct {
		name         string
		in           func(ws *workspace.Workspace) workspace.CreateProjectInput
		wantErr      error
		wantNotAsked bool
	}{
		{"an inactive team", func(ws *workspace.Workspace) workspace.CreateProjectInput {
			return workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: "tem_gone", Slug: "checkout", DisplayName: "Checkout"}
		}, workspace.ErrInactiveTeam, false},
		{"a missing team", func(ws *workspace.Workspace) workspace.CreateProjectInput {
			return workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), Slug: "checkout", DisplayName: "Checkout"}
		}, workspace.ErrInvalid, true},
		{"an invalid slug", func(ws *workspace.Workspace) workspace.CreateProjectInput {
			return workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: "Bad Slug", DisplayName: "Checkout"}
		}, workspace.ErrInvalid, true},
		{"a workspace that does not exist", func(*workspace.Workspace) workspace.CreateProjectInput {
			return workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: "wsp_gone", OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"}
		}, workspace.ErrNotFound, false},
	}
	for _, tc := range refused {
		t.Run(tc.name+" is refused", func(t *testing.T) {
			w := newWorld(t)
			ws := w.workspace(t, "platform", false)
			_, err := w.svc.CreateProject(ctx, alice, tc.in(ws))
			require.ErrorIs(t, err, tc.wantErr)
			assert.Empty(t, w.Projects)
			assert.Empty(t, w.AuditLog)
			if tc.wantNotAsked {
				assert.Empty(t, w.authz.Asked, "the authorizer is not asked about requests that can never succeed")
			}
		})
	}

	t.Run("an archived workspace is refused", func(t *testing.T) {
		w := newWorld(t)
		ws := w.workspace(t, "platform", true)
		_, err := w.svc.CreateProject(ctx, alice, workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"})
		require.ErrorIs(t, err, workspace.ErrArchived)
		assert.Empty(t, w.Resources)
		assert.Empty(t, w.Events)
	})

	t.Run("created under its workspace", func(t *testing.T) {
		w := newWorld(t)
		ws := w.workspace(t, "platform", false)

		p := w.project(t, ws, "checkout")

		assert.True(t, id.Valid(workspace.Project{}.Prefix(), p.ID()), "the domain assigns a prefixed ID: %q", p.ID())
		assert.Equal(t, testTeam, p.OwnerTeamID())
		assert.Equal(t, alice.PrincipalID, p.CreatedByPrincipalID())
		assert.Equal(t, workspacetest.ResourceCall{Op: "register", Resource: iam.Resource{ID: p.ID(), OrganizationID: org, Type: iam.ResourceProject, ParentID: ws.ID()}}, w.LastResource())
		assert.Equal(t, []string{"workspace.v1.project.created"}, w.EventTypes())
		e := w.LastAudit()
		assert.Equal(t, workspace.EventProjectCreated, e.Type)
		assert.Equal(t, p.Snapshot(), e.After)
		assert.Equal(t, "dec-"+ws.ID(), e.DecisionID)
		assert.Equal(t, fixedNow, e.OccurredAt)
	})
}

func TestUpdateProject(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	p := w.project(t, w.workspace(t, "platform", false), "checkout")
	w.reset()

	got, err := w.svc.UpdateProject(ctx, alice, org, p.ID(), workspace.UpdateProjectProps{DisplayName: new("Payments")})

	require.NoError(t, err)
	assert.Equal(t, "Payments", got.DisplayName())
	e := w.LastAudit()
	assert.Equal(t, workspace.EventProjectUpdated, e.Type)
	assert.Equal(t, "checkout", e.Before.(workspace.ProjectSnapshot).DisplayName)
	assert.Equal(t, "dec-"+p.ID(), e.DecisionID)
	assert.Equal(t, []string{"workspace.v1.project.updated"}, w.EventTypes())

	t.Run("invalid input is refused before asking", func(t *testing.T) {
		w.authz.Asked = nil
		_, err := w.svc.UpdateProject(ctx, alice, org, p.ID(), workspace.UpdateProjectProps{Slug: new("Bad Slug")})
		require.ErrorIs(t, err, workspace.ErrInvalid)
		assert.Empty(t, w.authz.Asked)
	})
}

func TestTransferProject(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	p := w.project(t, w.workspace(t, "platform", false), "checkout")
	w.reset()

	_, err := w.svc.TransferProject(ctx, alice, org, p.ID(), "tem_gone")
	require.ErrorIs(t, err, workspace.ErrInactiveTeam)
	_, err = w.svc.TransferProject(ctx, alice, org, p.ID(), "")
	require.ErrorIs(t, err, workspace.ErrInvalid)

	got, err := w.svc.TransferProject(ctx, alice, org, p.ID(), "tem_2")
	require.NoError(t, err)
	assert.Equal(t, "tem_2", got.OwnerTeamID())
	e := w.LastAudit()
	assert.Equal(t, workspace.EventProjectOwnerChanged, e.Type)
	assert.Equal(t, testTeam, e.Before.(workspace.ProjectSnapshot).OwnerTeamID)
	assert.Equal(t, "tem_2", e.After.(workspace.ProjectSnapshot).OwnerTeamID)
	assert.Equal(t, []string{"workspace.v1.project.transferred"}, w.EventTypes())
}

func TestArchiveAndRestoreProject(t *testing.T) {
	ctx := context.Background()

	t.Run("a project with active dependents is refused", func(t *testing.T) {
		w := newWorld(t, workspacetest.Dependent{Deps: []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: 3}}})
		p := w.project(t, w.workspace(t, "platform", false), "checkout")

		_, err := w.svc.ArchiveProject(ctx, alice, org, p.ID())

		require.ErrorIs(t, err, workspace.ErrHasDependents)
		assert.Contains(t, err.Error(), "3 active Application (delivery)")
	})

	t.Run("a dependent that fails stops the archive", func(t *testing.T) {
		w := newWorld(t, workspacetest.Dependent{Err: errBoom})
		p := w.project(t, w.workspace(t, "platform", false), "checkout")

		_, err := w.svc.ArchiveProject(ctx, alice, org, p.ID())

		require.ErrorIs(t, err, errBoom)
	})

	w := newWorld(t)
	ws := w.workspace(t, "platform", false)
	p := w.project(t, ws, "checkout")
	w.reset()

	got, err := w.svc.ArchiveProject(ctx, alice, org, p.ID())
	require.NoError(t, err)
	assert.True(t, got.Archived())
	assert.Equal(t, workspacetest.ResourceCall{Op: "archive", Resource: iam.Resource{ID: p.ID(), OrganizationID: org}}, w.LastResource())
	assert.Equal(t, workspace.EventProjectArchived, w.LastAudit().Type)
	assert.Equal(t, []string{"workspace.v1.project.archived"}, w.EventTypes())

	t.Run("archiving again does nothing", func(t *testing.T) {
		w.reset()
		_, err := w.svc.ArchiveProject(ctx, alice, org, p.ID())
		require.NoError(t, err)
		assert.Empty(t, w.AuditLog)
		assert.Empty(t, w.Resources)
	})

	t.Run("an archived project cannot be changed", func(t *testing.T) {
		_, err := w.svc.UpdateProject(ctx, alice, org, p.ID(), workspace.UpdateProjectProps{DisplayName: new("X")})
		require.ErrorIs(t, err, workspace.ErrArchived)
	})

	t.Run("restoring a project that does not exist is refused before asking", func(t *testing.T) {
		w.authz.Asked = nil
		_, err := w.svc.RestoreProject(ctx, alice, org, "prj_gone")
		require.ErrorIs(t, err, workspace.ErrNotFound)
		assert.Empty(t, w.authz.Asked)
	})

	t.Run("restoring into an archived workspace is refused", func(t *testing.T) {
		w.archiveWorkspace(t, ws, true)
		_, err := w.svc.RestoreProject(ctx, alice, org, p.ID())
		require.ErrorIs(t, err, workspace.ErrArchived)
		w.archiveWorkspace(t, ws, false)
	})

	t.Run("restored and authorized on the workspace", func(t *testing.T) {
		w.reset()
		got, err := w.svc.RestoreProject(ctx, alice, org, p.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, ws.ID(), w.authz.Last().ResourceID, "authorized on the workspace, since grants on an archived project do not apply")
		assert.Equal(t, workspacetest.ResourceCall{Op: "restore", Resource: iam.Resource{ID: p.ID(), OrganizationID: org}}, w.LastResource())
		assert.Equal(t, workspace.EventProjectRestored, w.LastAudit().Type)
		assert.Equal(t, "dec-"+ws.ID(), w.LastAudit().DecisionID)
		assert.Equal(t, []string{"workspace.v1.project.restored"}, w.EventTypes())
	})

	t.Run("restoring an active project does nothing", func(t *testing.T) {
		w.reset()
		_, err := w.svc.RestoreProject(ctx, alice, org, p.ID())
		require.NoError(t, err)
		assert.Empty(t, w.AuditLog)
	})
}

func TestRestoreProjectIsRefusedWhileTheSlugIsTaken(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	ws := w.workspace(t, "platform", false)
	p := w.project(t, ws, "checkout")
	_, err := w.svc.ArchiveProject(ctx, alice, org, p.ID())
	require.NoError(t, err)
	w.project(t, ws, "checkout")
	w.reset()

	_, err = w.svc.RestoreProject(ctx, alice, org, p.ID())

	require.ErrorIs(t, err, workspace.ErrSlugTaken)
	assert.Empty(t, w.Resources, "the resource is not restored when the project is not")
	assert.Empty(t, w.Events)
}

func TestAuditFailureFailsTheChange(t *testing.T) {
	w := newWorld(t)
	ws := w.workspace(t, "platform", false)
	w.AuditErr = errBoom

	_, err := w.svc.CreateProject(context.Background(), alice, workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"})

	require.ErrorIs(t, err, errBoom, "the transaction rolls back when the audit record cannot be written")
	assert.Empty(t, w.Projects)
	assert.Empty(t, w.Resources)
}
