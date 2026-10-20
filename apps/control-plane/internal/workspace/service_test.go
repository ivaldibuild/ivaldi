package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func TestNewServiceRequiresCollaborators(t *testing.T) {
	_, err := NewService(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is required")
}

func TestProvisionOrganization(t *testing.T) {
	f := newFixture(t)

	w := f.defaultWorkspace(t)

	assert.True(t, w.IsDefault())
	assert.Equal(t, DefaultWorkspaceSlug, w.Slug())
	assert.Equal(t, []resourceCall{{"register", iam.Resource{ID: w.ID(), OrganizationID: testOrg, Type: iam.ResourceWorkspace, ParentID: testOrg}}}, f.resources.calls)
	e := f.audit.last()
	assert.Equal(t, EventWorkspaceCreated, e.Type)
	assert.Equal(t, identity.ActorSystem, e.Actor.Kind)
	assert.Equal(t, fixedNow, e.OccurredAt, "audit records use the domain clock")
	assert.Equal(t, w.Data(), e.After)
	assert.Empty(t, f.authz.requests, "provisioning is called by IAM, not authorized per actor")
}

func TestCreateWorkspace(t *testing.T) {
	valid := CreateWorkspaceInput{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform"}

	tests := []struct {
		name    string
		in      CreateWorkspaceInput
		decide  func(authorization.Request) authorization.Decision
		setup   func(*fixture)
		wantErr error
	}{
		{name: "invalid slug", in: CreateWorkspaceInput{OrganizationID: testOrg, Slug: "Platform!", DisplayName: "P"}, wantErr: ErrInvalid},
		{name: "missing display name", in: CreateWorkspaceInput{OrganizationID: testOrg, Slug: "platform"}, wantErr: ErrInvalid},
		{name: "missing organization", in: CreateWorkspaceInput{Slug: "platform", DisplayName: "P"}, wantErr: ErrInvalid},
		{
			name: "denied",
			in:   valid,
			decide: func(authorization.Request) authorization.Decision {
				return authorization.Decision{ID: "dec_1", Outcome: authorization.OutcomeDeny}
			},
			wantErr: authorization.ErrDenied,
		},
		{
			name: "needs approval",
			in:   valid,
			decide: func(authorization.Request) authorization.Decision {
				return authorization.Decision{ID: "dec_1", Outcome: authorization.OutcomeRequiresApproval}
			},
			wantErr: authorization.ErrApprovalRequired,
		},
		{
			name:    "slug taken",
			in:      valid,
			setup:   func(f *fixture) { f.workspace(t, "platform") },
			wantErr: ErrSlugTaken,
		},
		{name: "created", in: valid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			f.authz.decide = tt.decide
			f.resources.calls = nil

			w, err := f.svc.CreateWorkspace(context.Background(), alice, tt.in)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, f.resources.calls, "nothing is registered when creation fails")
				return
			}
			require.NoError(t, err)
			assert.True(t, id.Valid(Workspace{}.Prefix(), w.ID()), "the domain assigns a prefixed ID: %q", w.ID())
			assert.Equal(t, alice.PrincipalID, w.CreatedByPrincipalID())
			assert.Equal(t, fixedNow, w.CreatedAt())
			assert.Equal(t, PermissionWorkspaceManage, f.authz.last().Permission)
			assert.Equal(t, testOrg, f.authz.last().ResourceID, "workspaces are created under the organization")
			assert.Equal(t, []resourceCall{{"register", iam.Resource{ID: w.ID(), OrganizationID: testOrg, Type: iam.ResourceWorkspace, ParentID: testOrg}}}, f.resources.calls)

			stored, err := f.repo.GetWorkspace(context.Background(), testOrg, w.ID())
			require.NoError(t, err)
			assert.Equal(t, w.Data(), stored.Data(), "the repository stores the workspace as the domain made it")
		})
	}
}

func TestInvalidInputIsRefusedBeforeAuthorizing(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.CreateWorkspace(context.Background(), alice, CreateWorkspaceInput{OrganizationID: testOrg, Slug: "Bad Slug", DisplayName: "P"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = f.svc.UpdateWorkspace(context.Background(), alice, testOrg, "wsp_1", UpdateWorkspaceProps{Slug: new("Bad Slug")})
	require.ErrorIs(t, err, ErrInvalid)

	assert.Empty(t, f.authz.requests, "the authorizer is not asked about requests that can never succeed")
}

func TestUpdateWorkspaceRenamesTheDefault(t *testing.T) {
	f := newFixture(t)
	def := f.defaultWorkspace(t)

	w, err := f.svc.UpdateWorkspace(context.Background(), alice, testOrg, def.ID(), UpdateWorkspaceProps{Slug: new("main"), DisplayName: new("Main")})

	require.NoError(t, err)
	assert.Equal(t, "main", w.Slug())
	assert.True(t, w.IsDefault())
	e := f.audit.last()
	assert.Equal(t, EventWorkspaceUpdated, e.Type)
	assert.Equal(t, "default", e.Before.(WorkspaceData).Slug)
	assert.Equal(t, "main", e.After.(WorkspaceData).Slug)
}

func TestUpdateArchivedWorkspaceIsRefused(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	w := f.workspace(t, "old")
	_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())
	require.NoError(t, err)

	_, err = f.svc.UpdateWorkspace(ctx, alice, testOrg, w.ID(), UpdateWorkspaceProps{DisplayName: new("New")})

	require.ErrorIs(t, err, ErrArchived)
}

func TestSetDefaultWorkspace(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	old := f.defaultWorkspace(t)
	next := f.workspace(t, "platform")

	got, err := f.svc.SetDefaultWorkspace(ctx, alice, testOrg, next.ID())

	require.NoError(t, err)
	assert.True(t, got.IsDefault())
	stored, err := f.repo.GetWorkspace(ctx, testOrg, old.ID())
	require.NoError(t, err)
	assert.False(t, stored.IsDefault(), "the old default is cleared")
	e := f.audit.last()
	assert.Equal(t, EventWorkspaceDefaultMoved, e.Type)
	assert.Equal(t, old.ID(), e.Before)
	assert.Equal(t, next.ID(), e.After)

	t.Run("setting the current default again does nothing", func(t *testing.T) {
		events := len(f.audit.events)
		_, err := f.svc.SetDefaultWorkspace(ctx, alice, testOrg, next.ID())
		require.NoError(t, err)
		assert.Len(t, f.audit.events, events)
	})
	t.Run("the old default can now be archived", func(t *testing.T) {
		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, old.ID())
		require.NoError(t, err)
	})
	t.Run("an archived workspace cannot become the default", func(t *testing.T) {
		_, err := f.svc.SetDefaultWorkspace(ctx, alice, testOrg, old.ID())
		require.ErrorIs(t, err, ErrArchived)
	})
}

func TestArchiveWorkspace(t *testing.T) {
	ctx := context.Background()

	t.Run("the default workspace is refused", func(t *testing.T) {
		f := newFixture(t)
		def := f.defaultWorkspace(t)
		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, def.ID())
		require.ErrorIs(t, err, ErrDefaultWorkspace)
	})

	t.Run("a workspace with active projects is refused", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		f.project(t, w, "checkout")

		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())

		require.ErrorIs(t, err, ErrHasDependents)
		assert.Contains(t, err.Error(), "1 active Project (workspace)")
	})

	t.Run("a workspace with active dependents is refused", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		f.withDependents(
			fakeDependent{deps: []Dependency{{Capability: "delivery", Kind: "Environment", Count: 0}}},
			fakeDependent{deps: []Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}},
		)

		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())

		var depErr *DependentsError
		require.ErrorAs(t, err, &depErr)
		assert.Equal(t, []Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}, depErr.Dependencies, "dependencies with nothing active are left out")
	})

	t.Run("a dependent that fails stops the archive", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		f.withDependents(fakeDependent{err: errBoom})

		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("archived, with its resource and an audit record", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")

		got, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())

		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, fixedNow, *got.ArchivedAt())
		assert.Equal(t, resourceCall{"archive", iam.Resource{ID: w.ID(), OrganizationID: testOrg}}, f.resources.calls[len(f.resources.calls)-1])
		assert.Equal(t, EventWorkspaceArchived, f.audit.last().Type)

		t.Run("archiving again does nothing", func(t *testing.T) {
			events := len(f.audit.events)
			_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())
			require.NoError(t, err)
			assert.Len(t, f.audit.events, events)
		})
	})
}

func TestRestoreWorkspace(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	w := f.workspace(t, "platform")
	_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())
	require.NoError(t, err)

	t.Run("refused while another workspace uses the slug", func(t *testing.T) {
		other := f.workspace(t, "platform")
		_, err := f.svc.RestoreWorkspace(ctx, alice, testOrg, w.ID())
		require.ErrorIs(t, err, ErrSlugTaken)
		_, err = f.svc.UpdateWorkspace(ctx, alice, testOrg, other.ID(), UpdateWorkspaceProps{Slug: new("platform-2")})
		require.NoError(t, err)
	})

	t.Run("restored once the slug is free", func(t *testing.T) {
		got, err := f.svc.RestoreWorkspace(ctx, alice, testOrg, w.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, testOrg, f.authz.last().ResourceID, "authorized at organization level, since grants on an archived workspace do not apply")
		assert.Equal(t, resourceCall{"restore", iam.Resource{ID: w.ID(), OrganizationID: testOrg}}, f.resources.calls[len(f.resources.calls)-1])
		assert.Equal(t, EventWorkspaceRestored, f.audit.last().Type)
	})
}

func TestCreateProject(t *testing.T) {
	ctx := context.Background()

	t.Run("an inactive team is refused", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		_, err := f.svc.CreateProject(ctx, alice, CreateProjectInput{OrganizationID: testOrg, WorkspaceID: w.ID(), OwnerTeamID: "tem_gone", Slug: "checkout", DisplayName: "Checkout"})
		require.ErrorIs(t, err, ErrInactiveTeam)
	})

	t.Run("a missing team is refused", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		_, err := f.svc.CreateProject(ctx, alice, CreateProjectInput{OrganizationID: testOrg, WorkspaceID: w.ID(), Slug: "checkout", DisplayName: "Checkout"})
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("a workspace that does not exist is refused", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.CreateProject(ctx, alice, CreateProjectInput{OrganizationID: testOrg, WorkspaceID: "wsp_gone", OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"})
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("an archived workspace is refused", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")
		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())
		require.NoError(t, err)
		_, err = f.svc.CreateProject(ctx, alice, CreateProjectInput{OrganizationID: testOrg, WorkspaceID: w.ID(), OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout"})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("created under its workspace", func(t *testing.T) {
		f := newFixture(t)
		w := f.workspace(t, "platform")

		p := f.project(t, w, "checkout")

		assert.True(t, id.Valid(Project{}.Prefix(), p.ID()), "the domain assigns a prefixed ID: %q", p.ID())
		assert.Equal(t, testTeam, p.OwnerTeamID())
		assert.Equal(t, w.ID(), f.authz.last().ResourceID, "projects are created under the workspace")
		assert.Equal(t, resourceCall{"register", iam.Resource{ID: p.ID(), OrganizationID: testOrg, Type: iam.ResourceProject, ParentID: w.ID()}}, f.resources.calls[len(f.resources.calls)-1])
		assert.Equal(t, EventProjectCreated, f.audit.last().Type)
		assert.Equal(t, p.Data(), f.audit.last().After)
	})
}

func TestUpdateProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.project(t, f.workspace(t, "platform"), "checkout")

	got, err := f.svc.UpdateProject(ctx, alice, testOrg, p.ID(), UpdateProjectProps{DisplayName: new("Payments")})

	require.NoError(t, err)
	assert.Equal(t, "Payments", got.DisplayName())
	e := f.audit.last()
	assert.Equal(t, EventProjectUpdated, e.Type)
	assert.Equal(t, "checkout", e.Before.(ProjectData).DisplayName)
	assert.Equal(t, p.ID(), f.authz.last().ResourceID)
}

func TestTransferProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.project(t, f.workspace(t, "platform"), "checkout")

	_, err := f.svc.TransferProject(ctx, alice, testOrg, p.ID(), "tem_gone")
	require.ErrorIs(t, err, ErrInactiveTeam)

	got, err := f.svc.TransferProject(ctx, alice, testOrg, p.ID(), "tem_2")
	require.NoError(t, err)
	assert.Equal(t, "tem_2", got.OwnerTeamID())
	e := f.audit.last()
	assert.Equal(t, EventProjectOwnerChanged, e.Type)
	assert.Equal(t, testTeam, e.Before.(ProjectData).OwnerTeamID)
	assert.Equal(t, "tem_2", e.After.(ProjectData).OwnerTeamID)
}

func TestArchiveAndRestoreProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	w := f.workspace(t, "platform")
	p := f.project(t, w, "checkout")

	f.withDependents(fakeDependent{deps: []Dependency{{Capability: "delivery", Kind: "Application", Count: 3}}})
	_, err := f.svc.ArchiveProject(ctx, alice, testOrg, p.ID())
	require.ErrorIs(t, err, ErrHasDependents)
	assert.Contains(t, err.Error(), "3 active Application (delivery)")

	f.withDependents()
	got, err := f.svc.ArchiveProject(ctx, alice, testOrg, p.ID())
	require.NoError(t, err)
	assert.True(t, got.Archived())
	assert.Equal(t, resourceCall{"archive", iam.Resource{ID: p.ID(), OrganizationID: testOrg}}, f.resources.calls[len(f.resources.calls)-1])

	t.Run("an archived project cannot be changed", func(t *testing.T) {
		_, err := f.svc.UpdateProject(ctx, alice, testOrg, p.ID(), UpdateProjectProps{DisplayName: new("X")})
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("restoring needs the project's own workspace", func(t *testing.T) {
		other := f.workspace(t, "data")
		_, err := f.svc.RestoreProject(ctx, alice, testOrg, other.ID(), p.ID())
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("restoring into an archived workspace is refused", func(t *testing.T) {
		// Archive the workspace while the project in it is archived.
		_, err := f.svc.ArchiveWorkspace(ctx, alice, testOrg, w.ID())
		require.NoError(t, err)
		_, err = f.svc.RestoreProject(ctx, alice, testOrg, w.ID(), p.ID())
		require.ErrorIs(t, err, ErrArchived)
		_, err = f.svc.RestoreWorkspace(ctx, alice, testOrg, w.ID())
		require.NoError(t, err)
	})

	t.Run("restored and authorized on the workspace", func(t *testing.T) {
		got, err := f.svc.RestoreProject(ctx, alice, testOrg, w.ID(), p.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, w.ID(), f.authz.last().ResourceID, "authorized on the workspace, since grants on an archived project do not apply")
	})
}

func TestAuditFailureFailsTheChange(t *testing.T) {
	f := newFixture(t)
	f.audit.err = errBoom

	_, err := f.svc.CreateWorkspace(context.Background(), alice, CreateWorkspaceInput{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform"})

	require.ErrorIs(t, err, errBoom, "the transaction rolls back when the audit record cannot be written")
}
