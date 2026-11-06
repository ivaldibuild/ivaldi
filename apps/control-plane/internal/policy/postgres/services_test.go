package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	outboxmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/locks"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacemigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/migrations"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/settings"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	org = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	key = "delivery.default_lifecycle"
)

var alice = identity.Actor{PrincipalID: "prn_alice", Kind: identity.ActorUser}

// allow allows everything, with a decision ID as short as a real one.
type allow struct{}

func (allow) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: "dcn_01ARZ3NDEKTSV4RRFFQ69G5FAV", Outcome: authorization.OutcomeAllow}, nil
}

// Locks added through the policy service govern the workspace capability's
// settings, wired as the control plane wires them, against PostgreSQL.
func TestLocksGovernSettingsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS, workspacemigrations.FS, outboxmigrations.FS, auditmigrations.FS)
	tx := database.NewTransactor(db)
	events, records := outboxpg.NewRepository(db), auditpg.NewRecorder(db)
	workspaceRepo := workspacepg.NewRepository(db)
	lockRepo := postgres.NewSettingLockRepository(db)

	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(workspace.Definition{
		Key: key, Levels: []workspace.Level{workspace.LevelOrganization, workspace.LevelWorkspace, workspace.LevelProject}, Permission: "lifecycle.manage",
	}))
	lockSource := policy.NewLockSource(lockRepo)
	lockSvc, err := locks.NewSettingLockService(
		locks.WithSettingLockRepo(lockRepo), locks.WithDefinitions(defs), locks.WithWorkspaces(workspace.NewLookups(workspaceRepo)),
		locks.WithAuthorizer(allow{}), locks.WithTransactor(tx), locks.WithOutboxRecorder(events), locks.WithAuditRecorder(records))
	require.NoError(t, err)
	settingSvc, err := settings.NewSettingService(
		settings.WithRepository(workspaceRepo), settings.WithDefinitions(defs), settings.WithLockSource(lockSource), settings.WithAuthorizer(allow{}),
		settings.WithTransactor(tx), settings.WithOutboxRecorder(events), settings.WithAuditRecorder(records))
	require.NoError(t, err)

	ws, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: "platform", DisplayName: "Platform"}, nil)
	require.NoError(t, err)
	require.NoError(t, workspaceRepo.CreateWorkspace(ctx, ws))
	p, err := workspace.NewProject(workspace.NewProjectProps{OrganizationID: org, WorkspaceID: ws.ID(), OwnerTeamID: "tem_1", Slug: "checkout", DisplayName: "Checkout"}, nil)
	require.NoError(t, err)
	require.NoError(t, workspaceRepo.CreateProject(ctx, p))

	orgScope := workspace.OrganizationScope(org)
	wsScope := workspace.WorkspaceScope(org, ws.ID())
	prjScope := workspace.ProjectScope(org, ws.ID(), p.ID())
	set := func(scope workspace.Scope, value string) {
		t.Helper()
		_, err := settingSvc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: scope, Key: key, Value: json.RawMessage(value)})
		require.NoError(t, err)
	}
	set(orgScope, `"lfc_org"`)
	set(prjScope, `"lfc_prj"`)

	_, err = lockSvc.AddSettingLock(ctx, alice, policy.SettingLockInput{Scope: policy.WorkspaceLockScope(org, ws.ID()), Key: key})
	require.NoError(t, err)

	resolved, err := settingSvc.ResolveSetting(ctx, alice, prjScope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_org"`, string(resolved.Value), "the project's value is below the lock, and the workspace has none")
	assert.True(t, resolved.Locked)

	_, err = settingSvc.ChangeSetting(ctx, alice, workspace.ChangeSettingInput{Scope: prjScope, Key: key, Value: json.RawMessage(`"lfc_x"`), ExpectedVersion: 1})
	require.ErrorIs(t, err, workspace.ErrLocked)
	set(wsScope, `"lfc_ws"`)

	require.NoError(t, lockSvc.RemoveSettingLock(ctx, alice, policy.SettingLockInput{Scope: policy.WorkspaceLockScope(org, ws.ID()), Key: key}))
	resolved, err = settingSvc.ResolveSetting(ctx, alice, prjScope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_prj"`, string(resolved.Value), "removing the lock brings the project's value back")
	assert.False(t, resolved.Locked)

	var eventTypes, auditTypes []string
	require.NoError(t, db.Raw(`SELECT type FROM outbox.events ORDER BY id`).Scan(&eventTypes).Error)
	require.NoError(t, db.Raw(`SELECT type FROM audit.records ORDER BY id`).Scan(&auditTypes).Error)
	assert.Equal(t, []string{
		"workspace.v1.setting.created", "workspace.v1.setting.created",
		"policy.v1.setting_lock.added", "workspace.v1.setting.created", "policy.v1.setting_lock.removed",
	}, eventTypes, "the refused change leaves nothing")
	assert.Equal(t, []string{
		workspace.EventSettingChanged, workspace.EventSettingChanged,
		policy.EventSettingLockAdded, workspace.EventSettingChanged, policy.EventSettingLockRemoved,
	}, auditTypes)
}
