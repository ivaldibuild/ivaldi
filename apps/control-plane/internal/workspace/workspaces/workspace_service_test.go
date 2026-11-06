package workspaces

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

func TestNewWorkspaceServiceRequiresEveryDependency(t *testing.T) {
	s := workspacetest.NewStore()
	all := allOptions(s, &workspacetest.Authorizer{})
	for i := range all {
		if i == 2 {
			continue // WithDependents is optional.
		}
		opts := append(append([]WorkspaceServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewWorkspaceService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewWorkspaceService(all...)
	assert.NoError(t, err)
}

func TestTheServiceRefusesAChangeThatWasNotAuthorized(t *testing.T) {
	s := workspacetest.NewStore()
	svc := &service{repo: s, resources: s, w: change.Writer{Tx: s, Outbox: s.Outbox(), Audit: s.Audit()}}

	_, err := svc.CreateWorkspace(context.Background(), alice, workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "platform", DisplayName: "Platform"})

	require.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Empty(t, s.Workspaces, "nothing is written without a decision")
}

// Every operation asks the authorizer the same question the workspace
// capability has always asked, about the same resource.
func TestOperationsAreAuthorizedOnTheirResource(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	ws := w.workspace(t, "platform")
	archived := w.workspace(t, "old")
	_, err := w.svc.ArchiveWorkspace(ctx, alice, org, archived.ID())
	require.NoError(t, err)
	create := workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "data", DisplayName: "Data"}
	update := workspace.UpdateWorkspaceProps{DisplayName: new("Platform 2")}

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
		resource   string
		proposal   any
	}{
		{"create", func() error { _, err := w.svc.CreateWorkspace(ctx, alice, create); return err },
			workspace.PermissionWorkspaceManage, workspace.ActionWorkspaceCreate, org, create},
		{"get", func() error { _, err := w.svc.GetWorkspace(ctx, alice, org, ws.ID()); return err },
			workspace.PermissionWorkspaceRead, workspace.ActionWorkspaceRead, ws.ID(), nil},
		{"list", func() error { _, err := w.svc.ListWorkspaces(ctx, alice, org, false); return err },
			workspace.PermissionWorkspaceRead, workspace.ActionWorkspaceList, org, nil},
		{"update", func() error { _, err := w.svc.UpdateWorkspace(ctx, alice, org, ws.ID(), update); return err },
			workspace.PermissionWorkspaceManage, workspace.ActionWorkspaceUpdate, ws.ID(), update},
		{"set default", func() error { _, err := w.svc.SetDefaultWorkspace(ctx, alice, org, ws.ID()); return err },
			workspace.PermissionWorkspaceManage, workspace.ActionWorkspaceSetDefault, org, ws.ID()},
		{"archive", func() error { _, err := w.svc.ArchiveWorkspace(ctx, alice, org, archived.ID()); return err },
			workspace.PermissionWorkspaceManage, workspace.ActionWorkspaceArchive, archived.ID(), nil},
		{"restore", func() error { _, err := w.svc.RestoreWorkspace(ctx, alice, org, archived.ID()); return err },
			workspace.PermissionWorkspaceManage, workspace.ActionWorkspaceRestore, org, archived.ID()},
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

func TestCreateWorkspace(t *testing.T) {
	valid := workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "platform", DisplayName: "Platform"}

	tests := []struct {
		name    string
		in      workspace.CreateWorkspaceInput
		decide  func(authorization.Request) authorization.Decision
		setup   func(*testing.T, *world)
		wantErr error
	}{
		{name: "invalid slug", in: workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "Platform!", DisplayName: "P"}, wantErr: workspace.ErrInvalid},
		{name: "missing display name", in: workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "platform"}, wantErr: workspace.ErrInvalid},
		{name: "missing organization", in: workspace.CreateWorkspaceInput{Slug: "platform", DisplayName: "P"}, wantErr: workspace.ErrInvalid},
		{name: "denied", in: valid, decide: workspacetest.Deny, wantErr: authorization.ErrDenied},
		{
			name: "needs approval",
			in:   valid,
			decide: func(authorization.Request) authorization.Decision {
				return authorization.Decision{ID: "dec_1", Outcome: authorization.OutcomeRequiresApproval}
			},
			wantErr: authorization.ErrApprovalRequired,
		},
		{name: "slug taken", in: valid, setup: func(t *testing.T, w *world) { w.workspace(t, "platform") }, wantErr: workspace.ErrSlugTaken},
		{name: "created", in: valid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			if tt.setup != nil {
				tt.setup(t, w)
			}
			w.authz.Decide = tt.decide
			w.reset()

			ws, err := w.svc.CreateWorkspace(context.Background(), alice, tt.in)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, w.Resources, "nothing is registered when creation fails")
				assert.Empty(t, w.Events)
				assert.Empty(t, w.AuditLog)
				return
			}
			require.NoError(t, err)
			assert.True(t, id.Valid(workspace.Workspace{}.Prefix(), ws.ID()), "the domain assigns a prefixed ID: %q", ws.ID())
			assert.Equal(t, alice.PrincipalID, ws.CreatedByPrincipalID())
			assert.Equal(t, fixedNow, ws.CreatedAt())
			assert.Equal(t, []workspacetest.ResourceCall{{Op: "register", Resource: iam.Resource{ID: ws.ID(), OrganizationID: org, Type: iam.ResourceWorkspace, ParentID: org}}}, w.Resources)
			assert.Equal(t, []string{"workspace.v1.workspace.created"}, w.EventTypes())

			e := w.LastAudit()
			assert.Equal(t, workspace.EventWorkspaceCreated, e.Type)
			assert.Equal(t, "dec-"+org, e.DecisionID, "the audit record names the decision")
			assert.Equal(t, alice, e.Actor)
			assert.Equal(t, fixedNow, e.OccurredAt)
			assert.Equal(t, ws.Snapshot(), e.After)

			stored, err := w.GetWorkspace(context.Background(), org, ws.ID())
			require.NoError(t, err)
			assert.Equal(t, ws.Snapshot(), stored.Snapshot(), "the repository stores the workspace as the domain made it")
		})
	}
}

func TestInvalidInputIsRefusedBeforeAuthorizing(t *testing.T) {
	w := newWorld(t)

	_, err := w.svc.CreateWorkspace(context.Background(), alice, workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "Bad Slug", DisplayName: "P"})
	require.ErrorIs(t, err, workspace.ErrInvalid)
	_, err = w.svc.UpdateWorkspace(context.Background(), alice, org, "wsp_1", workspace.UpdateWorkspaceProps{Slug: new("Bad Slug")})
	require.ErrorIs(t, err, workspace.ErrInvalid)

	assert.Empty(t, w.authz.Asked, "the authorizer is not asked about requests that can never succeed")
}

func TestUpdateWorkspaceRenamesTheDefault(t *testing.T) {
	w := newWorld(t)
	def := w.defaultWorkspace(t)
	w.reset()

	got, err := w.svc.UpdateWorkspace(context.Background(), alice, org, def.ID(), workspace.UpdateWorkspaceProps{Slug: new("main"), DisplayName: new("Main")})

	require.NoError(t, err)
	assert.Equal(t, "main", got.Slug())
	assert.True(t, got.IsDefault())
	e := w.LastAudit()
	assert.Equal(t, workspace.EventWorkspaceUpdated, e.Type)
	assert.Equal(t, "default", e.Before.(workspace.WorkspaceSnapshot).Slug)
	assert.Equal(t, "main", e.After.(workspace.WorkspaceSnapshot).Slug)
	assert.Equal(t, "dec-"+def.ID(), e.DecisionID)
	assert.Equal(t, []string{"workspace.v1.workspace.updated"}, w.EventTypes())
}

func TestUpdateArchivedWorkspaceIsRefused(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	ws := w.workspace(t, "old")
	_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())
	require.NoError(t, err)

	_, err = w.svc.UpdateWorkspace(ctx, alice, org, ws.ID(), workspace.UpdateWorkspaceProps{DisplayName: new("New")})

	require.ErrorIs(t, err, workspace.ErrArchived)
}

func TestSetDefaultWorkspace(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	old := w.defaultWorkspace(t)
	next := w.workspace(t, "platform")
	w.reset()

	got, err := w.svc.SetDefaultWorkspace(ctx, alice, org, next.ID())

	require.NoError(t, err)
	assert.True(t, got.IsDefault())
	stored, err := w.GetWorkspace(ctx, org, old.ID())
	require.NoError(t, err)
	assert.False(t, stored.IsDefault(), "the old default is cleared")
	e := w.LastAudit()
	assert.Equal(t, workspace.EventWorkspaceDefaultMoved, e.Type)
	assert.Equal(t, old.ID(), e.Before)
	assert.Equal(t, next.ID(), e.After)
	assert.Equal(t, "dec-"+org, e.DecisionID)
	assert.Equal(t, []string{"workspace.v1.workspace.default_cleared", "workspace.v1.workspace.made_default"}, w.EventTypes())

	t.Run("setting the current default again does nothing", func(t *testing.T) {
		w.reset()
		_, err := w.svc.SetDefaultWorkspace(ctx, alice, org, next.ID())
		require.NoError(t, err)
		assert.Empty(t, w.AuditLog)
		assert.Empty(t, w.Events)
	})
	t.Run("the old default can now be archived", func(t *testing.T) {
		_, err := w.svc.ArchiveWorkspace(ctx, alice, org, old.ID())
		require.NoError(t, err)
	})
	t.Run("an archived workspace cannot become the default", func(t *testing.T) {
		_, err := w.svc.SetDefaultWorkspace(ctx, alice, org, old.ID())
		require.ErrorIs(t, err, workspace.ErrArchived)
	})
}

func TestArchiveWorkspace(t *testing.T) {
	ctx := context.Background()

	t.Run("the default workspace is refused", func(t *testing.T) {
		w := newWorld(t)
		def := w.defaultWorkspace(t)
		_, err := w.svc.ArchiveWorkspace(ctx, alice, org, def.ID())
		require.ErrorIs(t, err, workspace.ErrDefaultWorkspace)
	})

	t.Run("a workspace with active projects is refused", func(t *testing.T) {
		w := newWorld(t)
		ws := w.workspace(t, "platform")
		w.project(t, ws, "checkout")

		_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())

		require.ErrorIs(t, err, workspace.ErrHasDependents)
		assert.Contains(t, err.Error(), "1 active Project (workspace)")
	})

	t.Run("a workspace with active dependents is refused", func(t *testing.T) {
		w := newWorld(t,
			workspacetest.Dependent{Deps: []workspace.Dependency{{Capability: "delivery", Kind: "Environment", Count: 0}}},
			workspacetest.Dependent{Deps: []workspace.Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}},
		)
		ws := w.workspace(t, "platform")

		_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())

		var depErr *workspace.DependentsError
		require.ErrorAs(t, err, &depErr)
		assert.Equal(t, []workspace.Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}, depErr.Dependencies, "dependencies with nothing active are left out")
	})

	t.Run("a dependent that fails stops the archive", func(t *testing.T) {
		w := newWorld(t, workspacetest.Dependent{Err: errBoom})
		ws := w.workspace(t, "platform")

		_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("archived, with its resource, event, and audit record", func(t *testing.T) {
		w := newWorld(t)
		ws := w.workspace(t, "platform")
		w.reset()

		got, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())

		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, fixedNow, *got.ArchivedAt())
		assert.Equal(t, workspacetest.ResourceCall{Op: "archive", Resource: iam.Resource{ID: ws.ID(), OrganizationID: org}}, w.LastResource())
		assert.Equal(t, []string{"workspace.v1.workspace.archived"}, w.EventTypes())
		e := w.LastAudit()
		assert.Equal(t, workspace.EventWorkspaceArchived, e.Type)
		assert.Equal(t, "dec-"+ws.ID(), e.DecisionID)

		t.Run("archiving again does nothing", func(t *testing.T) {
			w.reset()
			_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())
			require.NoError(t, err)
			assert.Empty(t, w.AuditLog)
			assert.Empty(t, w.Resources)
		})
	})
}

func TestRestoreWorkspace(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	ws := w.workspace(t, "platform")
	_, err := w.svc.ArchiveWorkspace(ctx, alice, org, ws.ID())
	require.NoError(t, err)

	t.Run("refused while another workspace uses the slug", func(t *testing.T) {
		other := w.workspace(t, "platform")
		w.reset()
		_, err := w.svc.RestoreWorkspace(ctx, alice, org, ws.ID())
		require.ErrorIs(t, err, workspace.ErrSlugTaken)
		assert.Empty(t, w.Resources, "the resource is not restored when the workspace is not")
		_, err = w.svc.UpdateWorkspace(ctx, alice, org, other.ID(), workspace.UpdateWorkspaceProps{Slug: new("platform-2")})
		require.NoError(t, err)
	})

	t.Run("restored once the slug is free", func(t *testing.T) {
		w.reset()
		got, err := w.svc.RestoreWorkspace(ctx, alice, org, ws.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, workspacetest.ResourceCall{Op: "restore", Resource: iam.Resource{ID: ws.ID(), OrganizationID: org}}, w.LastResource())
		assert.Equal(t, workspace.EventWorkspaceRestored, w.LastAudit().Type)
		assert.Equal(t, []string{"workspace.v1.workspace.restored"}, w.EventTypes())
	})

	t.Run("restoring an active workspace does nothing", func(t *testing.T) {
		w.reset()
		_, err := w.svc.RestoreWorkspace(ctx, alice, org, ws.ID())
		require.NoError(t, err)
		assert.Empty(t, w.AuditLog)
	})
}

func TestAuditFailureFailsTheChange(t *testing.T) {
	w := newWorld(t)
	w.AuditErr = errBoom

	_, err := w.svc.CreateWorkspace(context.Background(), alice, workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "platform", DisplayName: "Platform"})

	require.ErrorIs(t, err, errBoom, "the transaction rolls back when the audit record cannot be written")
	assert.Empty(t, w.Workspaces)
	assert.Empty(t, w.Resources)
	assert.Empty(t, w.Events)
}
