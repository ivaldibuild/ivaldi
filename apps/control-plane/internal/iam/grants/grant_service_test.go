package grants

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

var ctx = context.Background()

func TestNewGrantServiceRequiresItsDependencies(t *testing.T) {
	w := newWorld(t)
	all := w.options(w.authz)
	for i := range all {
		without := append(append([]GrantServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewGrantService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

func TestCreateGrant(t *testing.T) {
	w := newWorld(t)
	expires := t0.Add(24 * time.Hour)

	e, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{
		OrganizationID: org, PrincipalID: grace, Role: iam.RoleAdmin, ResourceID: workspace, ExpiresAt: &expires,
	})
	require.NoError(t, err)
	assert.Equal(t, iam.RoleAdmin, e.RoleKey)
	assert.Equal(t, role(iam.RoleAdmin), e.Grant.RoleID)
	assert.Equal(t, workspace, e.Grant.ResourceID)
	assert.Equal(t, ada, e.Grant.GrantedByPrincipalID, "the actor is the granter")
	assert.Equal(t, &expires, e.Grant.ExpiresAt)
	require.NotNil(t, e.Principal)
	assert.Equal(t, "Grace", e.Principal.DisplayName)
	assert.Contains(t, w.grants, e.Grant.ID)

	t.Run("asks to manage grants on the resource", func(t *testing.T) {
		req := w.authz.asked[len(w.authz.asked)-1]
		assert.Equal(t, iam.PermissionGrantManage, req.Permission)
		assert.Equal(t, iam.ActionGrantCreate, req.Action)
		assert.Equal(t, workspace, req.ResourceID)
		assert.NotNil(t, req.Proposal)
	})

	t.Run("records the event and the audit record", func(t *testing.T) {
		assert.Equal(t, []string{"iam.v1.grant.created"}, w.eventTypes())
		require.Len(t, w.audit, 1)
		a := w.audit[0]
		assert.Equal(t, iam.EventGrantCreated, a.Type)
		assert.Equal(t, e.Grant.ID, a.ResourceID)
		assert.Equal(t, adaActor, a.Actor)
		assert.Equal(t, "dec-"+iam.ActionGrantCreate, a.DecisionID)
		assert.Nil(t, a.Before)
		assert.Equal(t, e.Grant, a.After)
	})
}

func TestCreateGrantToAGroupOrATeam(t *testing.T) {
	w := newWorld(t)

	e, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, GroupID: deployers, Role: iam.RoleMember, ResourceID: project})
	require.NoError(t, err)
	require.NotNil(t, e.Group)
	assert.Equal(t, deployers, e.Group.ID)

	e, err = w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, TeamID: platform, Role: iam.RoleViewer, ResourceID: workspace})
	require.NoError(t, err)
	require.NotNil(t, e.Group)
	assert.Equal(t, teamGroup, e.Group.ID, "the Team's group")
	assert.Nil(t, e.Principal)
}

func TestOnlyOwnersGrantOrRemoveTheOwnerRole(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: grace, Role: iam.RoleOwner, ResourceID: org})
	require.NoError(t, err)
	assert.Equal(t, iam.PermissionOwnerManage, w.authz.asked[len(w.authz.asked)-1].Permission)

	require.NoError(t, w.svc.RemoveGrant(ctx, adaActor, org, w.owner.ID()))
	req := w.authz.asked[len(w.authz.asked)-1]
	assert.Equal(t, iam.PermissionOwnerManage, req.Permission)
	assert.Equal(t, iam.ActionGrantRemove, req.Action)
	assert.Equal(t, org, req.ResourceID)

	w.authz.deny = func(r authorization.Request) bool { return r.Permission == iam.PermissionOwnerManage }
	_, err = w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: bot, Role: iam.RoleOwner, ResourceID: org})
	assert.ErrorIs(t, err, authorization.ErrDenied, "someone who may only manage grants cannot make an owner")
}

func TestCreateGrantRefusals(t *testing.T) {
	cases := map[string]struct {
		in   iam.CreateGrantInput
		deny bool
		want error
	}{
		"an unknown role":              {in: iam.CreateGrantInput{PrincipalID: grace, Role: "superuser", ResourceID: org}, want: iam.ErrInvalid},
		"no grantee":                   {in: iam.CreateGrantInput{Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"two grantees":                 {in: iam.CreateGrantInput{PrincipalID: grace, GroupID: deployers, Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"owner below the Organization": {in: iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleOwner, ResourceID: workspace}, want: iam.ErrInvalid},
		"admin on a Project":           {in: iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleAdmin, ResourceID: project}, want: iam.ErrInvalid},
		"an unknown resource":          {in: iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleMember, ResourceID: "wsp_01JZ0000000000000000000099"}, want: iam.ErrInvalid},
		"an archived resource":         {in: iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleMember, ResourceID: archived}, want: iam.ErrStatus},
		"an unknown principal":         {in: iam.CreateGrantInput{PrincipalID: "prn_01JZ0000000000000000000099", Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"a suspended principal":        {in: iam.CreateGrantInput{PrincipalID: linus, Role: iam.RoleMember, ResourceID: org}, want: iam.ErrStatus},
		"an Agent Session":             {in: iam.CreateGrantInput{PrincipalID: session, Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"an unknown group":             {in: iam.CreateGrantInput{GroupID: "grp_01JZ0000000000000000000099", Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"an unknown Team":              {in: iam.CreateGrantInput{TeamID: "tem_01JZ0000000000000000000099", Role: iam.RoleMember, ResourceID: org}, want: iam.ErrInvalid},
		"an archived Team":             {in: iam.CreateGrantInput{TeamID: "tem_archived", Role: iam.RoleMember, ResourceID: org}, want: iam.ErrStatus},
		"a grant that already exists":  {in: iam.CreateGrantInput{PrincipalID: ada, Role: iam.RoleOwner, ResourceID: org}, want: iam.ErrAlreadyExists},
		"an expiry in the past": {
			in:   iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleMember, ResourceID: org, ExpiresAt: new(t0.Add(-time.Hour))},
			want: iam.ErrInvalid,
		},
		"denied by the authorizer": {in: iam.CreateGrantInput{PrincipalID: grace, Role: iam.RoleMember, ResourceID: org}, deny: true, want: authorization.ErrDenied},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			if tc.deny {
				w.authz.deny = func(authorization.Request) bool { return true }
			}
			tc.in.OrganizationID = org
			_, err := w.svc.CreateGrant(ctx, adaActor, tc.in)
			assert.ErrorIs(t, err, tc.want)
			assert.Len(t, w.grants, 1, "only the owner grant")
			assert.Empty(t, w.events)
			assert.Empty(t, w.audit)
		})
	}
}

func TestRemoveGrant(t *testing.T) {
	w := newWorld(t)
	e, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: grace, Role: iam.RoleMember, ResourceID: workspace})
	require.NoError(t, err)
	w.events, w.audit = nil, nil

	require.NoError(t, w.svc.RemoveGrant(ctx, adaActor, org, e.Grant.ID))
	assert.NotContains(t, w.grants, e.Grant.ID)
	req := w.authz.asked[len(w.authz.asked)-1]
	assert.Equal(t, iam.PermissionGrantManage, req.Permission)
	assert.Equal(t, workspace, req.ResourceID, "asked about the grant's resource")

	assert.Equal(t, []string{"iam.v1.grant.removed"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	a := w.audit[0]
	assert.Equal(t, iam.EventGrantRemoved, a.Type)
	assert.Equal(t, e.Grant, a.Before)
	assert.Nil(t, a.After)
	assert.Equal(t, "dec-"+iam.ActionGrantRemove, a.DecisionID)
	assert.Zero(t, w.orgLocks, "only an owner grant locks the Organization")

	assert.ErrorIs(t, w.svc.RemoveGrant(ctx, adaActor, org, e.Grant.ID), iam.ErrNotFound)
}

func TestTheLastOwnerStays(t *testing.T) {
	w := newWorld(t)
	assert.ErrorIs(t, w.svc.RemoveGrant(ctx, adaActor, org, w.owner.ID()), iam.ErrLastOwner)
	assert.Contains(t, w.grants, w.owner.ID())
	assert.Equal(t, 1, w.orgLocks, "counted with the Organization locked")
	assert.Empty(t, w.audit)

	t.Run("a suspended owner does not count", func(t *testing.T) {
		_, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: grace, Role: iam.RoleOwner, ResourceID: org})
		require.NoError(t, err)
		p := w.principals[grace]
		p.Status = iam.PrincipalSuspended
		w.principals[grace] = p
		assert.ErrorIs(t, w.svc.RemoveGrant(ctx, adaActor, org, w.owner.ID()), iam.ErrLastOwner)
	})

	t.Run("with another active owner it may go", func(t *testing.T) {
		p := w.principals[grace]
		p.Status = iam.PrincipalActive
		w.principals[grace] = p
		require.NoError(t, w.svc.RemoveGrant(ctx, adaActor, org, w.owner.ID()))
	})
}

func TestListGrants(t *testing.T) {
	w := newWorld(t)
	onWorkspace, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: grace, Role: iam.RoleAdmin, ResourceID: workspace})
	require.NoError(t, err)
	onProject, err := w.svc.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, GroupID: deployers, Role: iam.RoleMember, ResourceID: project})
	require.NoError(t, err)

	ids := func(q iam.ListResourceGrantsQuery) []string {
		t.Helper()
		q.OrganizationID = org
		r, err := w.svc.ListGrants(ctx, adaActor, q)
		require.NoError(t, err)
		var out []string
		for _, e := range r.Items {
			out = append(out, e.Grant.ID)
		}
		return out
	}
	assert.Equal(t, []string{onProject.Grant.ID}, ids(iam.ListResourceGrantsQuery{ResourceID: project}))
	assert.ElementsMatch(t, []string{onProject.Grant.ID, onWorkspace.Grant.ID, w.owner.ID()},
		ids(iam.ListResourceGrantsQuery{ResourceID: project, IncludeAncestors: true}), "the grants that apply to the Project")

	req := w.authz.asked[len(w.authz.asked)-1]
	assert.Equal(t, iam.PermissionGrantRead, req.Permission)
	assert.Equal(t, project, req.ResourceID)

	_, err = w.svc.ListGrants(ctx, adaActor, iam.ListResourceGrantsQuery{OrganizationID: org, ResourceID: "prj_01JZ0000000000000000000099"})
	assert.ErrorIs(t, err, iam.ErrNotFound)
}

// The service under the authorizing wrapper cannot change anything on its
// own: its audit record needs a decision.
func TestChangesWithoutADecisionFail(t *testing.T) {
	w := newWorld(t)
	inner := w.svc.(*authorized).next
	_, err := inner.CreateGrant(ctx, adaActor, iam.CreateGrantInput{OrganizationID: org, PrincipalID: grace, Role: iam.RoleMember, ResourceID: org})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Len(t, w.grants, 1)
}
