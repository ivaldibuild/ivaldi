package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	outboxmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/approvals"
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

// fakeIAM is what IAM tells approvals: everyone holds every permission on
// the Organization, each resource sits directly under it, and one agent
// Session acts for ada.
type fakeIAM struct{ session *iam.AgentSession }

func (fakeIAM) HeldResources(context.Context, iam.HeldResourcesQuery) ([]string, error) {
	return []string{org}, nil
}

func (fakeIAM) ResourceAncestors(_ context.Context, _, resourceID string) ([]string, error) {
	return []string{resourceID, org}, nil
}

func (f *fakeIAM) Get(context.Context, string, string) (*iam.AgentSession, error) {
	return f.session, nil
}

// recordingHandler is a subject's capability: it records what policy asks.
type recordingHandler struct{ calls []string }

func (h *recordingHandler) Lock(_ context.Context, _, subjectID string) error {
	h.calls = append(h.calls, "lock "+subjectID)
	return nil
}

func (h *recordingHandler) Approve(_ context.Context, _ identity.Actor, r *policy.ApprovalRequest) error {
	h.calls = append(h.calls, "approve "+r.Subject().ID)
	return nil
}

func (h *recordingHandler) Reject(_ context.Context, _ identity.Actor, r *policy.ApprovalRequest) error {
	h.calls = append(h.calls, "reject "+r.Subject().ID)
	return nil
}

// An agent Session's request is opened, listed for those who could decide
// it, refused while the Session is revoked, and approved once, against
// PostgreSQL.
func TestApprovalsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS, outboxmigrations.FS, auditmigrations.FS)
	tx := database.NewTransactor(db)
	repo := postgres.NewApprovalRequestRepository(db)
	ada := identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: identity.ActorUser}
	agent := identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAS", Kind: identity.ActorAgentSession, SessionID: "ses_1"}
	// agentSession is the agent's Session, acting for ada, revoked at revokedAt.
	agentSession := func(revokedAt *time.Time) *iam.AgentSession {
		t.Helper()
		created := time.Now().Add(-time.Minute)
		s, err := iam.NewAgentSession(iam.NewAgentSessionProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: org, DisplayName: "Fix the build", Status: iam.PrincipalActive, CreatedAt: &created, UpdatedAt: &created},
			SessionID:      agent.SessionID, WorkItemID: "wki_1", DelegatorPrincipalID: ada.PrincipalID,
			ExpiresAt: created.Add(time.Hour), RevokedAt: revokedAt, ScopeResourceIDs: []string{org},
		}, &agent.PrincipalID)
		require.NoError(t, err)
		return s
	}
	people := &fakeIAM{session: agentSession(nil)}
	handler := &recordingHandler{}
	subjects := policy.NewSubjects()
	require.NoError(t, subjects.Register(policy.SubjectType{Type: "delivery.deployment", Handler: handler}))
	common := []approvals.Option{
		approvals.WithApprovalRequestRepo(repo), approvals.WithSubjects(subjects), approvals.WithHeldResources(people),
		approvals.WithResourceAncestry(people), approvals.WithAgentSessions(people), approvals.WithAuthorizer(allow{}),
		approvals.WithTransactor(tx), approvals.WithOutboxRecorder(outboxpg.NewRepository(db)), approvals.WithAuditRecorder(auditpg.NewRecorder(db)),
	}
	recorder, err := approvals.NewApprovals(common...)
	require.NoError(t, err)
	svc, err := approvals.NewService(common...)
	require.NoError(t, err)

	opened, err := recorder.Open(ctx, policy.OpenApprovalInput{
		Subject: policy.Subject{Type: "delivery.deployment", ID: "dep_1"},
		Title:   "Deploy api 1.4.0 to Production",
		Request: authorization.Request{Actor: agent, Permission: "deployment.create", OrganizationID: org,
			ResourceID: "env_prod", Action: "deployment.create", Proposal: map[string]string{"target": "tgt_1"}},
		Decision:       authorization.Decision{ID: authorization.NewDecisionID(), Rule: "builtin.test", Reason: "an agent asked"},
		Requirements:   []policy.Requirement{{Permission: "deployment.create", ResourceID: "env_prod"}},
		ReadResourceID: "prj_web",
		Summary:        map[string]string{"environment_id": "env_prod"},
	})
	require.NoError(t, err)
	assert.Equal(t, new(ada.PrincipalID), opened.Snapshot().OnBehalfOfPrincipalID, "IAM names who the session acts for")

	list := func(actor identity.Actor) []policy.ApprovalRequestView {
		t.Helper()
		page, err := svc.ListApprovalRequests(ctx, actor, policy.ListApprovalRequestsInput{OrganizationID: org})
		require.NoError(t, err)
		return page.Items
	}
	pending := list(alice)
	require.Len(t, pending, 1)
	assert.True(t, pending[0].CanDecide)
	assert.Empty(t, list(ada), "the person the session acts for cannot decide it")
	assert.Empty(t, list(identity.Actor{PrincipalID: "prn_ci", Kind: identity.ActorServiceAccount}), "only users decide")
	view, err := svc.GetApprovalRequest(ctx, ada, org, opened.ID())
	require.NoError(t, err)
	assert.False(t, view.CanDecide, "the requester's side can still read it")

	decide := policy.DecideApprovalInput{OrganizationID: org, RequestID: opened.ID(), Note: "ship it"}
	people.session = agentSession(new(time.Now()))
	_, err = svc.ApproveRequest(ctx, alice, decide)
	require.ErrorIs(t, err, policy.ErrSessionRevoked)
	assert.Equal(t, []string{"lock dep_1"}, handler.calls, "a revoked session's work is not applied")

	people.session = agentSession(nil)
	approved, err := svc.ApproveRequest(ctx, alice, decide)
	require.NoError(t, err)
	assert.Equal(t, policy.ApprovalApproved, approved.State)
	assert.Equal(t, []string{"lock dep_1", "lock dep_1", "approve dep_1"}, handler.calls, "the subject is locked before it is applied")
	_, err = svc.RejectRequest(ctx, alice, policy.DecideApprovalInput{OrganizationID: org, RequestID: opened.ID(), Note: "too late"})
	assert.ErrorIs(t, err, policy.ErrNotPending)
	assert.Empty(t, list(alice), "nothing is left pending")

	var auditTypes []string
	require.NoError(t, db.Raw(`SELECT type FROM audit.records ORDER BY id`).Scan(&auditTypes).Error)
	assert.Equal(t, []string{policy.EventApprovalRequestCreated, policy.EventApprovalRequestApproved}, auditTypes,
		"refused decisions leave nothing")
}
