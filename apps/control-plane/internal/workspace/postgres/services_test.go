package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	auditmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	outboxmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	teamapi "github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/projects"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/settings"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspaces"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org  = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	team = "tem_1"
)

var alice = identity.Actor{PrincipalID: "prn_alice", Kind: identity.ActorUser}

// registrar records IAM resources in memory; IAM's tables are not needed here.
type registrar struct{ calls []string }

func (r *registrar) RegisterResource(_ context.Context, res iam.Resource) error {
	r.calls = append(r.calls, "register "+res.ID)
	return nil
}

func (r *registrar) ArchiveResource(_ context.Context, _, id string) error {
	r.calls = append(r.calls, "archive "+id)
	return nil
}

func (r *registrar) RestoreResource(_ context.Context, _, id string) error {
	r.calls = append(r.calls, "restore "+id)
	return nil
}

// allow allows everything, with a decision ID as short as a real one.
type allow struct{}

func (allow) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: "dec_01ARZ3NDEKTSV4RRFFQ69G5FAV", Outcome: authorization.OutcomeAllow}, nil
}

type teams struct{}

func (teams) IsActiveTeam(_ context.Context, _, teamID string) (bool, error) {
	return teamID == team || teamID == "tem_other", nil
}

func (teams) ListTeamsForMembers(context.Context, string, []string) (map[string][]teamapi.Membership, error) {
	return map[string][]teamapi.Membership{}, nil
}

// stack is the workspace capability wired to PostgreSQL, as the control
// plane wires it, with the outbox and audit log in the same database.
type stack struct {
	db          *gorm.DB
	tx          *database.Transactor
	repo        workspace.Repository
	provisioner iam.OrganizationProvisioner
	workspaces  workspace.WorkspaceService
	projects    workspace.ProjectService
	settings    workspace.SettingService
}

func newStack(t *testing.T) *stack {
	t.Helper()
	db := databasetest.New(t, migrations.FS, outboxmigrations.FS, auditmigrations.FS)
	tx := database.NewTransactor(db)
	repo := postgres.NewRepository(db)
	events := outboxpg.NewRepository(db)
	records := auditpg.NewRecorder(db)
	resources := &registrar{}
	authz := allow{}

	p, err := workspace.NewProvisioner(repo, tx, resources, events, records)
	require.NoError(t, err)
	ws, err := workspaces.NewWorkspaceService(
		workspaces.WithRepository(repo), workspaces.WithResourceRegistrar(resources), workspaces.WithAuthorizer(authz),
		workspaces.WithTransactor(tx), workspaces.WithOutboxRecorder(events), workspaces.WithAuditRecorder(records))
	require.NoError(t, err)
	ps, err := projects.NewProjectService(
		projects.WithRepository(repo), projects.WithResourceRegistrar(resources), projects.WithTeams(teams{}), projects.WithAuthorizer(authz),
		projects.WithTransactor(tx), projects.WithOutboxRecorder(events), projects.WithAuditRecorder(records))
	require.NoError(t, err)
	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(workspace.Definition{
		Key: "delivery.default_lifecycle", Levels: []workspace.Level{workspace.LevelOrganization, workspace.LevelWorkspace, workspace.LevelProject},
		Default: json.RawMessage(`"lfc_basic"`), Permission: "delivery.settings.manage",
	}))
	ss, err := settings.NewSettingService(
		settings.WithRepository(repo), settings.WithDefinitions(defs), settings.WithLockSource(workspace.NoLocks()), settings.WithAuthorizer(authz),
		settings.WithTransactor(tx), settings.WithOutboxRecorder(events), settings.WithAuditRecorder(records))
	require.NoError(t, err)
	return &stack{db: db, tx: tx, repo: repo, provisioner: p, workspaces: ws, projects: ps, settings: ss}
}

func (s *stack) column(t *testing.T, query string) []string {
	t.Helper()
	var out []string
	require.NoError(t, s.db.Raw(query).Scan(&out).Error)
	return out
}

func TestTheCapabilityAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)

	require.NoError(t, s.provisioner.ProvisionOrganization(ctx, org))
	def, err := s.repo.GetDefaultWorkspace(ctx, org)
	require.NoError(t, err)

	ws, err := s.workspaces.CreateWorkspace(ctx, alice, workspace.CreateWorkspaceInput{OrganizationID: org, Slug: "platform", DisplayName: "Platform"})
	require.NoError(t, err)
	_, err = s.workspaces.SetDefaultWorkspace(ctx, alice, org, ws.ID())
	require.NoError(t, err)
	_, err = s.workspaces.ArchiveWorkspace(ctx, alice, org, def.ID())
	require.NoError(t, err, "the old default can be archived once another is the default")

	p, err := s.projects.CreateProject(ctx, alice, workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: team, Slug: "checkout", DisplayName: "Checkout"})
	require.NoError(t, err)
	_, err = s.workspaces.ArchiveWorkspace(ctx, alice, org, ws.ID())
	require.ErrorIs(t, err, workspace.ErrDefaultWorkspace)

	scope := workspace.ProjectScope(org, ws.ID(), p.ID())
	key := "delivery.default_lifecycle"
	_, err = s.settings.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: key, Value: json.RawMessage(`"lfc_std"`)})
	require.NoError(t, err)
	changed, err := s.settings.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: key, Value: json.RawMessage(`"lfc_fast"`), ExpectedVersion: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), changed.Version())
	_, err = s.settings.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: key, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
	require.ErrorIs(t, err, workspace.ErrConflict)

	resolved, err := s.settings.ResolveSetting(ctx, alice, scope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_fast"`, string(resolved.Value))
	require.NoError(t, s.settings.RemoveSetting(ctx, alice, workspace.RemoveSettingInput{Scope: scope, Key: key, ExpectedVersion: 2}))

	_, err = s.projects.ArchiveProject(ctx, alice, org, p.ID())
	require.NoError(t, err)
	_, err = s.projects.RestoreProject(ctx, alice, org, p.ID())
	require.NoError(t, err)

	assert.Equal(t, []string{
		"workspace.v1.workspace.created", "workspace.v1.workspace.created",
		"workspace.v1.workspace.default_cleared", "workspace.v1.workspace.made_default", "workspace.v1.workspace.archived",
		"workspace.v1.project.created",
		"workspace.v1.setting.created", "workspace.v1.setting.changed", "workspace.v1.setting.removed",
		"workspace.v1.project.archived", "workspace.v1.project.restored",
	}, s.column(t, `SELECT type FROM outbox.events ORDER BY id`), "every change's events commit with it, and refused ones leave none")
	assert.Equal(t, []string{
		workspace.EventWorkspaceCreated, workspace.EventWorkspaceCreated, workspace.EventWorkspaceDefaultMoved, workspace.EventWorkspaceArchived,
		workspace.EventProjectCreated, workspace.EventSettingChanged, workspace.EventSettingChanged, workspace.EventSettingRemoved,
		workspace.EventProjectArchived, workspace.EventProjectRestored,
	}, s.column(t, `SELECT type FROM audit.records ORDER BY id`))
	assert.Equal(t, []string{"system"}, s.column(t, `SELECT actor_kind FROM audit.records WHERE decision_id IS NULL`), "only provisioning is recorded without a decision")
}

func TestTheProvisionerJoinsTheCallersTransaction(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	errOrganization := errors.New("organization not created")

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, s.provisioner.ProvisionOrganization(ctx, org))
		return errOrganization
	})

	require.ErrorIs(t, err, errOrganization)
	_, err = s.repo.GetDefaultWorkspace(ctx, org)
	require.ErrorIs(t, err, workspace.ErrNotFound, "the default workspace rolls back with the organization")
	assert.Empty(t, s.column(t, `SELECT type FROM outbox.events`))
	assert.Empty(t, s.column(t, `SELECT type FROM audit.records`))
}

func TestListProjectsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	require.NoError(t, s.provisioner.ProvisionOrganization(ctx, org))
	ws, err := s.repo.GetDefaultWorkspace(ctx, org)
	require.NoError(t, err)
	create := func(slug, name, owner string) *workspace.Project {
		t.Helper()
		p, err := s.projects.CreateProject(ctx, alice, workspace.CreateProjectInput{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: owner, Slug: slug, DisplayName: name})
		require.NoError(t, err)
		return p
	}
	checkout := create("checkout", "Checkout", team)
	billing := create("billing", "Billing 100%", team)
	search := create("search", "Search", "tem_other")
	_, err = s.projects.ArchiveProject(ctx, alice, org, search.ID())
	require.NoError(t, err)

	list := func(q workspace.ListProjectsQuery) []string {
		t.Helper()
		q.OrganizationID, q.WorkspaceID = org, ws.ID()
		page, err := s.projects.ListProjects(ctx, alice, q)
		require.NoError(t, err)
		ids := make([]string, len(page.Items))
		for i, p := range page.Items {
			ids[i] = p.ID()
		}
		return ids
	}

	assert.Equal(t, []string{checkout.ID(), billing.ID()}, list(workspace.ListProjectsQuery{}), "by creation, without archived ones")
	assert.Equal(t, []string{billing.ID(), checkout.ID(), search.ID()},
		list(workspace.ListProjectsQuery{IncludeArchived: true, Page: pagination.Query{SortBy: "display_name"}}))
	assert.Equal(t, []string{checkout.ID()}, list(workspace.ListProjectsQuery{Search: "CHECK"}), "names and slugs, ignoring case")
	assert.Equal(t, []string{billing.ID()}, list(workspace.ListProjectsQuery{Search: "100%"}), "a % matches itself")
	assert.Empty(t, list(workspace.ListProjectsQuery{Search: "_"}))
	assert.Equal(t, []string{search.ID()}, list(workspace.ListProjectsQuery{OwnerTeamID: "tem_other", IncludeArchived: true}))

	first, err := s.projects.ListProjects(ctx, alice, workspace.ListProjectsQuery{OrganizationID: org, WorkspaceID: ws.ID(), Page: pagination.Query{Limit: 1, SortBy: "slug"}})
	require.NoError(t, err)
	require.True(t, first.HasNext)
	assert.Equal(t, 2, first.TotalCount)
	assert.Equal(t, billing.ID(), first.Items[0].ID())
	second, err := s.projects.ListProjects(ctx, alice, workspace.ListProjectsQuery{OrganizationID: org, WorkspaceID: ws.ID(), Page: pagination.Query{Limit: 1, SortBy: "slug", NextCursor: first.NextCursor}})
	require.NoError(t, err)
	assert.Equal(t, checkout.ID(), second.Items[0].ID())

	_, err = s.projects.ListProjects(ctx, alice, workspace.ListProjectsQuery{OrganizationID: org, WorkspaceID: ws.ID(), Page: pagination.Query{SortBy: "owner"}})
	assert.ErrorIs(t, err, workspace.ErrInvalid)
}
