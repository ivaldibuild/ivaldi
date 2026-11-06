package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

func TestUserLastSignedInAt(t *testing.T) {
	f := newFixture(t)
	org := f.writeOrg(t, "acme")
	u := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, f.users.Write(f.ctx, u))
	assert.Nil(t, f.loadUser(t, u.ID()).LastSignedInAt(), "nil until the first sign-in")

	signedIn := at(t, time.Hour)
	u.RecordSignIn()
	require.NoError(t, f.users.Write(f.ctx, u))
	assert.Equal(t, &signedIn, f.loadUser(t, u.ID()).LastSignedInAt())
}

// writePerson writes a user named name with email in the access world's
// Organization, with status.
func (w *accessWorld) writePerson(t *testing.T, name, email string, status iam.PrincipalStatus) string {
	t.Helper()
	u := must(iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: w.org, DisplayName: name, Status: status},
		AccountID:      w.writeAccount(t, email).ID(),
	}, nil))
	require.NoError(t, w.users.Write(w.ctx, u))
	return u.ID()
}

// writeGrant grants role on resourceID to a principal or a group and
// returns the Grant.
func (w *accessWorld) writeGrant(t *testing.T, role, resourceID string, principalID, groupID *string, expires *time.Time) *iam.Grant {
	t.Helper()
	created := t0.Add(-48 * time.Hour)
	g := must(iam.NewGrant(iam.NewGrantProps{
		OrganizationID: w.org, PrincipalID: principalID, GroupID: groupID, RoleID: roleID(role),
		ResourceID: resourceID, GrantedByPrincipalID: w.ada, ExpiresAt: expires, CreatedAt: &created,
	}, nil))
	require.NoError(t, w.grants.Write(w.ctx, g))
	return g
}

func personIDs(r *pagination.Result[iam.Person]) []string {
	out := make([]string, len(r.Items))
	for i, p := range r.Items {
		out[i] = p.User.ID
	}
	return out
}

func TestListPeople(t *testing.T) {
	w := newAccessWorld(t) // Ada, displayed as "Member", is ada@example.com
	clock := at(t, 0)
	people := NewPeopleRepository(w.db)
	grace := w.writePerson(t, "Grace Hopper", "grace@navy.example.com", "")
	linus := w.writePerson(t, "Linus", "linus@example.com", iam.PrincipalSuspended)
	require.NoError(t, NewAgentRepository(w.db).Write(w.ctx, newAgent(w.org, "release-agent")))
	other := w.writeOrg(t, "globex")
	require.NoError(t, w.users.Write(w.ctx, newUser(other.ID(), w.writeAccount(t, "eve@example.com").ID())))

	w.writeGrant(t, iam.RoleOwner, w.org, &w.ada, nil, nil)
	admins := w.group(t, "admins", grace)
	w.writeGrant(t, iam.RoleAdmin, w.org, nil, &admins, nil)
	w.writeGrant(t, iam.RoleViewer, w.ws, &grace, nil, nil)
	past := clock.Add(-time.Minute)
	w.writeGrant(t, iam.RoleMember, w.org, &grace, nil, &past)

	list := func(q iam.ListPeopleQuery) *pagination.Result[iam.Person] {
		t.Helper()
		q.OrganizationID = w.org
		r, err := people.ListPeople(w.ctx, q)
		require.NoError(t, err)
		return r
	}

	t.Run("lists the active users, by display name", func(t *testing.T) {
		r := list(iam.ListPeopleQuery{})
		assert.Equal(t, []string{grace, w.ada}, personIDs(r), "Grace Hopper sorts before Member; no Agent, no other Organization")
		assert.Equal(t, 2, r.TotalCount)

		g := r.Items[0]
		assert.Equal(t, "grace@navy.example.com", g.Email)
		assert.Equal(t, "Grace Hopper", g.User.DisplayName)
		assert.Equal(t, []iam.HeldRole{{Key: iam.RoleAdmin, RoleID: roleID(iam.RoleAdmin), ResourceID: w.org}}, g.Roles,
			"the admin role through a group; not the Workspace role or the expired one")
		assert.Equal(t, []iam.HeldRole{{Key: iam.RoleOwner, RoleID: roleID(iam.RoleOwner), ResourceID: w.org}}, r.Items[1].Roles)
	})

	t.Run("includes inactive people when asked", func(t *testing.T) {
		assert.Equal(t, []string{grace, linus, w.ada}, personIDs(list(iam.ListPeopleQuery{IncludeInactive: true})))
	})

	t.Run("searches names and emails, ignoring case", func(t *testing.T) {
		assert.Equal(t, []string{grace}, personIDs(list(iam.ListPeopleQuery{Search: "HOPPER"})))
		assert.Equal(t, []string{grace}, personIDs(list(iam.ListPeopleQuery{Search: "navy.example"})))
		assert.Equal(t, []string{w.ada}, personIDs(list(iam.ListPeopleQuery{Search: "ada@"})))
		assert.Empty(t, personIDs(list(iam.ListPeopleQuery{Search: "%"})), "wildcards match themselves")
		assert.Empty(t, personIDs(list(iam.ListPeopleQuery{Search: "_"})))
	})

	t.Run("pages by cursor", func(t *testing.T) {
		first := list(iam.ListPeopleQuery{Page: pagination.Query{Limit: 1}})
		require.True(t, first.HasNext)
		assert.Equal(t, []string{grace}, personIDs(first))
		second := list(iam.ListPeopleQuery{Page: pagination.Query{Limit: 1, NextCursor: first.NextCursor}})
		assert.Equal(t, []string{w.ada}, personIDs(second))
		assert.False(t, second.HasNext)

		byCreation := list(iam.ListPeopleQuery{Page: pagination.Query{SortBy: "created_at", SortOrder: pagination.SortDescending}})
		assert.Equal(t, []string{grace, w.ada}, personIDs(byCreation))
	})

	t.Run("an invalid sort is invalid input", func(t *testing.T) {
		_, err := people.ListPeople(w.ctx, iam.ListPeopleQuery{OrganizationID: w.org, Page: pagination.Query{SortBy: "email"}})
		assert.ErrorIs(t, err, iam.ErrInvalid)
	})
}
