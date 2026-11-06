package workspace_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const org = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"

var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func newProvisioner(t *testing.T, s *workspacetest.Store) iam.OrganizationProvisioner {
	t.Helper()
	p, err := workspace.NewProvisioner(s, s, s, s.Outbox(), s.Audit())
	require.NoError(t, err)
	return p
}

func TestNewProvisionerRequiresEveryDependency(t *testing.T) {
	s := workspacetest.NewStore()
	for name, build := range map[string]func() (iam.OrganizationProvisioner, error){
		"repository": func() (iam.OrganizationProvisioner, error) {
			return workspace.NewProvisioner(nil, s, s, s.Outbox(), s.Audit())
		},
		"transactor": func() (iam.OrganizationProvisioner, error) {
			return workspace.NewProvisioner(s, nil, s, s.Outbox(), s.Audit())
		},
		"resources": func() (iam.OrganizationProvisioner, error) {
			return workspace.NewProvisioner(s, s, nil, s.Outbox(), s.Audit())
		},
		"outbox": func() (iam.OrganizationProvisioner, error) { return workspace.NewProvisioner(s, s, s, nil, s.Audit()) },
		"audit":  func() (iam.OrganizationProvisioner, error) { return workspace.NewProvisioner(s, s, s, s.Outbox(), nil) },
	} {
		_, err := build()
		assert.Error(t, err, "without the %s", name)
	}
}

func TestProvisionOrganization(t *testing.T) {
	clock.Fix(t, now)
	s := workspacetest.NewStore()

	require.NoError(t, newProvisioner(t, s).ProvisionOrganization(context.Background(), org))

	stored := slices.Collect(maps.Values(s.Workspaces))
	require.Len(t, stored, 1)
	w := stored[0]
	assert.True(t, w.IsDefault)
	assert.Equal(t, workspace.DefaultWorkspaceSlug, w.Slug)
	assert.Empty(t, w.CreatedByPrincipalID, "the system creates the default workspace")
	assert.Equal(t, []workspacetest.ResourceCall{{Op: "register", Resource: iam.Resource{ID: w.ID, OrganizationID: org, Type: iam.ResourceWorkspace, ParentID: org}}}, s.Resources)
	assert.Equal(t, []string{"workspace.v1.workspace.created"}, s.EventTypes())
	assert.Equal(t, workspace.AggregateWorkspace, s.Events[0].AggregateType)
	assert.Equal(t, w.ID, s.Events[0].AggregateID)

	require.Len(t, s.AuditLog, 1)
	e := s.AuditLog[0]
	assert.Equal(t, workspace.EventWorkspaceCreated, e.Type)
	assert.Equal(t, identity.Actor{Kind: identity.ActorSystem}, e.Actor)
	assert.Empty(t, e.DecisionID, "provisioning is called by IAM, not authorized per actor")
	assert.Equal(t, now, e.OccurredAt)
	assert.Equal(t, w, e.After)
}

func TestProvisionOrganizationRollsBackTogether(t *testing.T) {
	s := workspacetest.NewStore()
	s.AuditErr = errors.New("boom")

	err := newProvisioner(t, s).ProvisionOrganization(context.Background(), org)

	require.ErrorIs(t, err, s.AuditErr)
	assert.Empty(t, s.Workspaces, "the workspace is not kept without its audit record")
	assert.Empty(t, s.Resources)
	assert.Empty(t, s.Events)
}

func TestProvisionOrganizationJoinsTheCallersTransaction(t *testing.T) {
	s := workspacetest.NewStore()
	p := newProvisioner(t, s)
	errOrganization := errors.New("organization not created")

	err := s.WithinTx(context.Background(), func(ctx context.Context) error {
		require.NoError(t, p.ProvisionOrganization(ctx, org))
		return errOrganization
	})

	require.ErrorIs(t, err, errOrganization)
	assert.Empty(t, s.Workspaces, "the default workspace rolls back with the organization")
	assert.Empty(t, s.AuditLog)
	assert.Empty(t, s.Events)
}

func TestProvisionOrganizationTwiceIsRefused(t *testing.T) {
	s := workspacetest.NewStore()
	p := newProvisioner(t, s)
	require.NoError(t, p.ProvisionOrganization(context.Background(), org))

	err := p.ProvisionOrganization(context.Background(), org)

	require.Error(t, err, "an organization has one default workspace")
	assert.Len(t, s.Workspaces, 1)
}

func TestProvisionOrganizationNeedsAnOrganization(t *testing.T) {
	s := workspacetest.NewStore()
	require.ErrorIs(t, newProvisioner(t, s).ProvisionOrganization(context.Background(), ""), workspace.ErrInvalid)
}

func TestNoLocks(t *testing.T) {
	locked, err := workspace.NoLocks().LockedKeys(context.Background(), org, "wsp_1")
	require.NoError(t, err)
	assert.Empty(t, locked)
}

func TestSettingResolver(t *testing.T) {
	ctx := context.Background()
	const key = "delivery.default_lifecycle"
	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(workspace.Definition{
		Key: key, Levels: []workspace.Level{workspace.LevelOrganization, workspace.LevelWorkspace, workspace.LevelProject},
		Default: json.RawMessage(`"lfc_basic"`), Permission: "delivery.settings.manage",
	}))
	s := workspacetest.NewStore()
	store := func(scope workspace.Scope, v string) {
		setting, err := workspace.NewSetting(workspace.NewSettingProps{Scope: scope, Key: key, Value: json.RawMessage(v)})
		require.NoError(t, err)
		require.NoError(t, s.InsertSetting(ctx, setting))
	}
	store(workspace.OrganizationScope(org), `"lfc_org"`)
	store(workspace.WorkspaceScope(org, "wsp_1"), `"lfc_ws"`)
	store(workspace.ProjectScope(org, "wsp_1", "prj_1"), `"lfc_prj"`)
	projectScope := workspace.ProjectScope(org, "wsp_1", "prj_1")

	t.Run("requires its dependencies", func(t *testing.T) {
		_, err := workspace.NewSettingResolver(nil, defs, workspace.NoLocks())
		assert.Error(t, err)
		_, err = workspace.NewSettingResolver(s, nil, workspace.NoLocks())
		assert.Error(t, err)
		_, err = workspace.NewSettingResolver(s, defs, nil)
		assert.Error(t, err)
	})

	r, err := workspace.NewSettingResolver(s, defs, s)
	require.NoError(t, err)

	got, err := r.ResolveSetting(ctx, projectScope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_prj"`, string(got.Value))
	assert.Equal(t, workspace.LevelProject, got.Level)

	s.Locks[org+"/wsp_1"] = map[string]workspace.Level{key: workspace.LevelWorkspace}
	got, err = r.ResolveSetting(ctx, projectScope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_ws"`, string(got.Value), "a workspace lock ignores project values")
	assert.True(t, got.Locked)

	s.Locks[org+"/"] = map[string]workspace.Level{key: workspace.LevelOrganization}
	got, err = r.ResolveSetting(ctx, projectScope, key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_org"`, string(got.Value), "an organization lock wins over a workspace lock")

	got, err = r.ResolveSetting(ctx, workspace.WorkspaceScope(org, "wsp_2"), key)
	require.NoError(t, err)
	assert.JSONEq(t, `"lfc_org"`, string(got.Value), "a workspace without its own value gets the organization's")

	_, err = r.ResolveSetting(ctx, projectScope, "delivery.nope")
	require.ErrorIs(t, err, workspace.ErrUnknownSetting)
	_, err = r.ResolveSetting(ctx, workspace.Scope{OrganizationID: org, ProjectID: "prj_1"}, key)
	require.ErrorIs(t, err, workspace.ErrInvalid)
}

func TestCheckDependents(t *testing.T) {
	ctx := context.Background()
	target := workspace.ArchiveTarget{OrganizationID: org, WorkspaceID: "wsp_1"}

	require.NoError(t, workspace.CheckDependents(ctx, nil, target))

	err := workspace.CheckDependents(ctx, []workspace.Dependent{
		workspacetest.Dependent{Deps: []workspace.Dependency{{Capability: "delivery", Kind: "Environment", Count: 0}}},
		workspacetest.Dependent{Deps: []workspace.Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}},
	}, target)
	var depErr *workspace.DependentsError
	require.ErrorAs(t, err, &depErr)
	assert.Equal(t, []workspace.Dependency{{Capability: "delivery", Kind: "Lifecycle", Count: 2}}, depErr.Dependencies, "dependencies with nothing active are left out")

	boom := errors.New("boom")
	require.ErrorIs(t, workspace.CheckDependents(ctx, []workspace.Dependent{workspacetest.Dependent{Err: boom}}, target), boom)
}
