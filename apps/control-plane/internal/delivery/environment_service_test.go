package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

func TestServiceCreateEnvironment(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		f := newSvcFixture(t)

		e, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "Production", Tier: TierProduction, ExecutionEnabled: true,
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Environment{}.Prefix(), e.ID()), "the domain assigns a prefixed ID: %q", e.ID())
		assert.Equal(t, "production", e.Slug(), "the slug is derived from the display name")
		assert.Equal(t, svcNow, e.CreatedAt())
		assert.Equal(t, []fakeResourceCall{{"register", iam.Resource{ID: e.ID(), OrganizationID: svcOrg, Type: iam.ResourceEnvironment, ParentID: f.ws.ID()}}}, f.resources.all())
		ev := f.audit.last()
		assert.Equal(t, EventEnvironmentCreated, ev.Type)
		assert.Equal(t, e.ID(), ev.ResourceID)
		assert.Nil(t, ev.Before)
		assert.Equal(t, e.Snapshot(), ev.After)

		stored, err := f.store.GetEnvironment(svcCtx, svcOrg, e.ID())
		require.NoError(t, err)
		assert.Equal(t, e.Snapshot(), stored.Snapshot(), "the repository stores the environment as the domain made it")
	})

	refusals := []struct {
		name    string
		setup   func(*svcFixture) string
		slug    string
		wantErr error
	}{
		{"archived workspace", func(f *svcFixture) string {
			w, _ := f.addWorkspace(t, "old", "legacy")
			f.archiveWorkspace(t, w)
			return w.ID()
		}, "prod", ErrArchived},
		{"missing workspace", func(*svcFixture) string { return "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAW" }, "prod", ErrNotFound},
		{"unknown workspace ID", func(f *svcFixture) string { return f.ws.ID() + "x" }, "prod", ErrNotFound},
		{"slug taken", func(f *svcFixture) string { f.env(t, "prod"); return f.ws.ID() }, "prod", ErrSlugTaken},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			f := newSvcFixture(t)
			wsID := tt.setup(f)
			state, events, resources := f.marks()

			_, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: wsID, Slug: tt.slug, DisplayName: "Prod", Tier: TierProduction})

			require.ErrorIs(t, err, tt.wantErr)
			f.requireUnchanged(t, state, events, resources)
		})
	}

	t.Run("a slug used by an archived environment is free", func(t *testing.T) {
		f := newSvcFixture(t)
		old := f.env(t, "prod")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{old.ID()})
		require.NoError(t, err)

		e := f.env(t, "prod")
		assert.NotEqual(t, old.ID(), e.ID())
	})

	t.Run("the same slug in another workspace is fine", func(t *testing.T) {
		f := newSvcFixture(t)
		other, _ := f.addWorkspace(t, "data", "etl")
		f.env(t, "prod")
		f.envIn(t, other.ID(), "prod")
	})

	t.Run("a failing IAM registration rolls back the environment", func(t *testing.T) {
		f := newSvcFixture(t)
		state, events, _ := f.marks()
		f.resources.err = svcErrBoom

		_, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "Prod", Tier: TierProduction})

		require.ErrorIs(t, err, svcErrBoom)
		f.requireUnchanged(t, state, events, 0)
	})
}

func TestServiceGetAndListEnvironments(t *testing.T) {
	f := newSvcFixture(t)
	staging := f.env(t, "staging")
	prod := f.env(t, "prod")
	old := f.env(t, "old")
	_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{old.ID()})
	require.NoError(t, err)

	got, err := f.svc.GetEnvironment(svcCtx, svcAlice, svcOrg, staging.ID())
	require.NoError(t, err)
	assert.Equal(t, staging.Snapshot(), got.Snapshot())

	_, err = f.svc.GetEnvironment(svcCtx, svcAlice, svcOrg, "env_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.ErrorIs(t, err, ErrNotFound)

	t.Run("by slug", func(t *testing.T) {
		got, err := f.svc.GetEnvironmentBySlug(svcCtx, svcAlice, svcOrg, f.ws.ID(), "prod")
		require.NoError(t, err)
		assert.Equal(t, prod.ID(), got.ID())

		f.authz.reset()
		_, err = f.svc.GetEnvironmentBySlug(svcCtx, svcAlice, svcOrg, f.ws.ID(), "old")
		require.ErrorIs(t, err, ErrNotFound, "only active environments are found by slug")
		assert.Empty(t, f.authz.all(), "nothing to authorize on when nothing is found")
	})

	t.Run("list", func(t *testing.T) {
		page, err := f.svc.ListEnvironments(svcCtx, svcAlice, ListEnvironmentsQuery{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), Page: pagination.Query{Limit: 10}})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)

		page, err = f.svc.ListEnvironments(svcCtx, svcAlice, ListEnvironmentsQuery{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), IncludeArchived: true})
		require.NoError(t, err)
		assert.Len(t, page.Items, 3)
	})
}

func TestServiceUpdateEnvironment(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	f.env(t, "prod")

	got, err := f.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, e.ID(), UpdateEnvironmentProps{DisplayName: new("Stage"), ExecutionEnabled: new(true)})

	require.NoError(t, err)
	assert.Equal(t, "Stage", got.DisplayName())
	assert.True(t, got.ExecutionEnabled())
	ev := f.audit.last()
	assert.Equal(t, EventEnvironmentUpdated, ev.Type)
	assert.Equal(t, e.ID(), ev.ResourceID)
	assert.Equal(t, "staging", ev.Before.(EnvironmentSnapshot).DisplayName)
	assert.Equal(t, "Stage", ev.After.(EnvironmentSnapshot).DisplayName)

	t.Run("slug clash", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, e.ID(), UpdateEnvironmentProps{Slug: new("prod")})
		require.ErrorIs(t, err, ErrSlugTaken)
		f.requireUnchanged(t, state, events, resources)
	})
	t.Run("missing", func(t *testing.T) {
		_, err := f.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, "env_01ARZ3NDEKTSV4RRFFQ69G5FAW", UpdateEnvironmentProps{Slug: new("x")})
		require.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("archived", func(t *testing.T) {
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
		_, err = f.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, e.ID(), UpdateEnvironmentProps{DisplayName: new("X")})
		require.ErrorIs(t, err, ErrArchived)
	})
}

func TestServiceArchiveEnvironments(t *testing.T) {
	t.Run("archives each and its IAM resource", func(t *testing.T) {
		f := newSvcFixture(t)
		a := f.env(t, "a")
		b := f.env(t, "b")
		before := f.audit.count()
		f.resources.calls = nil

		got, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID(), a.ID()})

		require.NoError(t, err)
		require.Len(t, got, 2, "repeated IDs are archived once")
		for _, e := range got {
			assert.True(t, e.Archived())
			assert.Equal(t, svcNow, *e.DeletedAt())
		}
		assert.Equal(t, []fakeResourceCall{
			{"archive", iam.Resource{ID: a.ID(), OrganizationID: svcOrg}},
			{"archive", iam.Resource{ID: b.ID(), OrganizationID: svcOrg}},
		}, f.resources.all())
		events := f.audit.since(before)
		require.Len(t, events, 2)
		for i, e := range []*Environment{a, b} {
			assert.Equal(t, EventEnvironmentArchived, events[i].Type)
			assert.Equal(t, e.ID(), events[i].ResourceID)
			assert.Nil(t, events[i].Before.(EnvironmentSnapshot).DeletedAt)
			assert.NotNil(t, events[i].After.(EnvironmentSnapshot).DeletedAt)
		}
		assert.Len(t, f.authz.all(), 2+2, "each environment is authorized once")
	})

	t.Run("refused while a target is active", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		tg := f.target(t, e, "web")
		state, events, resources := f.marks()

		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)

		_, err = f.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, tg.ID())
		require.NoError(t, err)
		_, err = f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err, "archived targets do not hold the environment")
	})

	t.Run("refused while in an active lifecycle", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		l := f.lifecycle(t, "mainline", e)

		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.ErrorIs(t, err, ErrInUse)

		_, err = f.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
		require.NoError(t, err)
		_, err = f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err, "archived lifecycles do not hold the environment")
	})

	t.Run("all or nothing", func(t *testing.T) {
		f := newSvcFixture(t)
		free := f.env(t, "free")
		busy := f.env(t, "busy")
		f.target(t, busy, "web")
		state, events, resources := f.marks()

		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{free.ID(), busy.ID()})

		require.ErrorIs(t, err, ErrInUse)
		f.requireUnchanged(t, state, events, resources)
		stored, err := f.store.GetEnvironment(svcCtx, svcOrg, free.ID())
		require.NoError(t, err)
		assert.False(t, stored.Archived(), "the environment archived before the refusal is rolled back")
	})

	t.Run("a denial for one refuses all", func(t *testing.T) {
		f := newSvcFixture(t)
		a := f.env(t, "a")
		b := f.env(t, "b")
		state, events, resources := f.marks()
		f.authz.decide = func(r authorization.Request) authorization.Decision {
			if r.ResourceID == b.ID() {
				return fakeDecide(authorization.OutcomeDeny)(r)
			}
			return fakeDecide(authorization.OutcomeAllow)(r)
		}

		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID()})

		require.ErrorIs(t, err, authorization.ErrDenied)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("a missing environment refuses all", func(t *testing.T) {
		f := newSvcFixture(t)
		a := f.env(t, "a")
		state, events, resources := f.marks()

		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), "env_01ARZ3NDEKTSV4RRFFQ69G5FAW"})

		require.ErrorIs(t, err, ErrNotFound)
		f.requireUnchanged(t, state, events, resources)
	})
}

func TestServiceRestoreEnvironments(t *testing.T) {
	t.Run("restores each and its IAM resource", func(t *testing.T) {
		f := newSvcFixture(t)
		a := f.env(t, "a")
		b := f.env(t, "b")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID()})
		require.NoError(t, err)
		f.authz.reset()
		f.resources.calls = nil
		before := f.audit.count()

		got, err := f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID()})

		require.NoError(t, err)
		require.Len(t, got, 2)
		for _, e := range got {
			assert.False(t, e.Archived())
		}
		for _, r := range f.authz.all() {
			assert.Equal(t, f.ws.ID(), r.ResourceID, "grants on an archived environment do not apply, so the workspace is checked")
			assert.Equal(t, PermissionEnvironmentManage, r.Permission)
		}
		assert.Len(t, f.authz.all(), 2)
		assert.Equal(t, []fakeResourceCall{
			{"restore", iam.Resource{ID: a.ID(), OrganizationID: svcOrg}},
			{"restore", iam.Resource{ID: b.ID(), OrganizationID: svcOrg}},
		}, f.resources.all())
		assert.Equal(t, []string{EventEnvironmentRestored, EventEnvironmentRestored}, func() []string {
			var out []string
			for _, e := range f.audit.since(before) {
				out = append(out, e.Type)
			}
			return out
		}())
	})

	t.Run("refused in an archived workspace", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "a")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
		f.archiveWorkspace(t, f.ws)
		state, events, resources := f.marks()

		_, err = f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})

		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("an unknown ID refuses all before the transaction", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "a")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
		state, events, resources := f.marks()
		txCalls := f.tx.calls

		_, err = f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID(), "env_01ARZ3NDEKTSV4RRFFQ69G5FAW"})

		require.ErrorIs(t, err, ErrNotFound)
		f.requireUnchanged(t, state, events, resources)
		assert.Equal(t, txCalls, f.tx.calls)
	})

	t.Run("all or nothing", func(t *testing.T) {
		f := newSvcFixture(t)
		a := f.env(t, "a")
		b := f.env(t, "b")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID()})
		require.NoError(t, err)
		f.env(t, "b") // takes b's slug while it is archived
		state, events, resources := f.marks()

		_, err = f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{a.ID(), b.ID()})

		require.ErrorIs(t, err, ErrSlugTaken)
		f.requireUnchanged(t, state, events, resources)
	})
}

func TestServiceCreateTarget(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")

		tg, err := f.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{
			OrganizationID: svcOrg, EnvironmentID: e.ID(), DisplayName: "Web", Runtime: RuntimeKubernetes, Locator: svcLocator("web"),
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Target{}.Prefix(), tg.ID()))
		assert.Nil(t, tg.AgentPrincipalID(), "no agent until one is assigned")
		ev := f.audit.last()
		assert.Equal(t, EventTargetCreated, ev.Type)
		assert.Equal(t, tg.Snapshot(), ev.After)
		stored, err := f.store.GetTarget(svcCtx, svcOrg, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), stored.Snapshot())
	})

	t.Run("refused in an archived environment", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
		state, events, resources := f.marks()

		_, err = f.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{OrganizationID: svcOrg, EnvironmentID: e.ID(), DisplayName: "Web", Runtime: RuntimeKubernetes, Locator: svcLocator("web")})

		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("refused in a missing environment", func(t *testing.T) {
		f := newSvcFixture(t)
		_, err := f.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{OrganizationID: svcOrg, EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAW", DisplayName: "Web", Runtime: RuntimeKubernetes, Locator: svcLocator("web")})
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("slug taken in the environment", func(t *testing.T) {
		f := newSvcFixture(t)
		e := f.env(t, "staging")
		f.target(t, e, "web")
		_, err := f.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{OrganizationID: svcOrg, EnvironmentID: e.ID(), Slug: "web", DisplayName: "Web", Runtime: RuntimeKubernetes, Locator: svcLocator("web")})
		require.ErrorIs(t, err, ErrSlugTaken)
	})
}

func TestServiceTargetAgent(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	tg := f.target(t, e, "web")

	t.Run("only the assigned agent may report", func(t *testing.T) {
		err := f.svc.RecordTargetReport(svcCtx, svcAgent, svcOrg, tg.ID())
		require.ErrorIs(t, err, ErrNotTargetAgent, "no agent is assigned yet")
	})

	got, err := f.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, tg.ID(), svcAgent.PrincipalID)
	require.NoError(t, err)
	assert.Equal(t, svcAgent.PrincipalID, *got.AgentPrincipalID())
	assert.Equal(t, EventTargetAgentAssigned, f.audit.last().Type)

	t.Run("the agent reports without authorizing or auditing", func(t *testing.T) {
		f.authz.reset()
		events := f.audit.count()

		require.NoError(t, f.svc.RecordTargetReport(svcCtx, svcAgent, svcOrg, tg.ID()))

		assert.Empty(t, f.authz.all(), "being the assigned agent is the only authorization a report needs")
		assert.Equal(t, events, f.audit.count(), "reports are not audited")
		stored, err := f.store.GetTarget(svcCtx, svcOrg, tg.ID())
		require.NoError(t, err)
		require.NotNil(t, stored.LastReportedAt())
		assert.Equal(t, svcNow, *stored.LastReportedAt())
	})

	t.Run("another principal is refused", func(t *testing.T) {
		state, events, resources := f.marks()
		err := f.svc.RecordTargetReport(svcCtx, svcAlice, svcOrg, tg.ID())
		require.ErrorIs(t, err, ErrNotTargetAgent)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("a missing target", func(t *testing.T) {
		err := f.svc.RecordTargetReport(svcCtx, svcAgent, svcOrg, "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAW")
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("an empty principal unassigns", func(t *testing.T) {
		got, err := f.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, tg.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.AgentPrincipalID())
		assert.Nil(t, got.LastReportedAt())
		ev := f.audit.last()
		assert.Equal(t, EventTargetAgentAssigned, ev.Type)
		assert.Equal(t, new(svcAgent.PrincipalID), ev.Before.(TargetSnapshot).AgentPrincipalID)
		assert.Nil(t, ev.After.(TargetSnapshot).AgentPrincipalID)

		err = f.svc.RecordTargetReport(svcCtx, svcAgent, svcOrg, tg.ID())
		require.ErrorIs(t, err, ErrNotTargetAgent)
	})
}

func TestServiceUpdateTarget(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	tg := f.target(t, e, "web")

	got, err := f.svc.UpdateTarget(svcCtx, svcAlice, svcOrg, tg.ID(), UpdateTargetProps{Locator: svcLocator("web-2")})

	require.NoError(t, err)
	assert.Equal(t, "web-2", got.Locator()[LocatorNamespace])
	assert.Equal(t, EventTargetUpdated, f.audit.last().Type)

	_, err = f.svc.UpdateTarget(svcCtx, svcAlice, svcOrg, tg.ID(), UpdateTargetProps{Locator: map[string]string{LocatorCluster: "c"}})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestServiceArchiveAndRestoreTarget(t *testing.T) {
	f := newSvcFixture(t)
	e := f.env(t, "staging")
	tg := f.target(t, e, "web")

	got, err := f.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, tg.ID())
	require.NoError(t, err)
	assert.True(t, got.Archived())
	assert.Equal(t, EventTargetArchived, f.audit.last().Type)

	t.Run("refused while the environment is archived", func(t *testing.T) {
		_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)
		state, events, resources := f.marks()

		_, err = f.svc.RestoreTarget(svcCtx, svcAlice, svcOrg, tg.ID())

		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})

	t.Run("restored once the environment is", func(t *testing.T) {
		_, err := f.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{e.ID()})
		require.NoError(t, err)

		got, err := f.svc.RestoreTarget(svcCtx, svcAlice, svcOrg, tg.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		ev := f.audit.last()
		assert.Equal(t, EventTargetRestored, ev.Type)
		assert.NotNil(t, ev.Before.(TargetSnapshot).DeletedAt)
		assert.Nil(t, ev.After.(TargetSnapshot).DeletedAt)
	})

	t.Run("an archived target cannot report", func(t *testing.T) {
		_, err := f.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, tg.ID(), svcAgent.PrincipalID)
		require.NoError(t, err)
		_, err = f.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, tg.ID())
		require.NoError(t, err)

		err = f.svc.RecordTargetReport(svcCtx, svcAgent, svcOrg, tg.ID())
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("an archived target's agent cannot be changed", func(t *testing.T) {
		state, events, resources := f.marks()
		_, err := f.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, tg.ID(), "")
		require.ErrorIs(t, err, ErrArchived)
		_, err = f.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, tg.ID(), "prn_other")
		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})
}
