package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func TestGrantRepositoryReadsAndDeletes(t *testing.T) {
	w := newAccessWorld(t)
	now := at(t, 0)
	grace := w.writePerson(t, "Grace", "grace@example.com", "")
	linus := w.writePerson(t, "Linus", "linus@example.com", iam.PrincipalSuspended)
	deployers := w.group(t, "deployers", grace)

	owner := w.writeGrant(t, iam.RoleOwner, w.org, &w.ada, nil, nil)
	onWorkspace := w.writeGrant(t, iam.RoleAdmin, w.ws, nil, &deployers, nil)
	past := now.Add(-time.Minute)
	expired := w.writeGrant(t, iam.RoleMember, w.ws, &grace, nil, &past)

	t.Run("gets a grant", func(t *testing.T) {
		g, err := w.grants.Get(w.ctx, w.org, onWorkspace.ID())
		require.NoError(t, err)
		assert.Equal(t, onWorkspace.Snapshot(), g.Snapshot())
		_, err = w.grants.GetForUpdate(w.ctx, w.org, onWorkspace.ID())
		require.NoError(t, err)
		_, err = w.grants.Get(w.ctx, w.writeOrg(t, "globex").ID(), onWorkspace.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound, "not another Organization's")
	})

	t.Run("lists grants with their grantees named", func(t *testing.T) {
		r, err := w.grants.ListGrants(w.ctx, iam.ListGrantsQuery{OrganizationID: w.org, ResourceIDs: []string{w.ws, w.org}, At: now})
		require.NoError(t, err)
		require.Len(t, r.Items, 2, "the expired grant is left out")
		byID := map[string]iam.GrantEntry{}
		for _, e := range r.Items {
			byID[e.Grant.ID] = e
		}
		o := byID[owner.ID()]
		assert.Equal(t, iam.RoleOwner, o.RoleKey)
		require.NotNil(t, o.Principal)
		assert.Equal(t, iam.GranteePrincipal{ID: w.ada, Kind: iam.PrincipalUser, DisplayName: "Member", Status: iam.PrincipalActive}, *o.Principal)
		assert.Nil(t, o.Group)
		d := byID[onWorkspace.ID()]
		require.NotNil(t, d.Group)
		assert.Equal(t, "deployers", d.Group.Name)
		assert.Equal(t, iam.GroupSource("manual"), d.Group.Source)
		assert.Nil(t, d.Principal)

		r, err = w.grants.ListGrants(w.ctx, iam.ListGrantsQuery{OrganizationID: w.org, ResourceIDs: []string{w.ws}, IncludeExpired: true, At: now})
		require.NoError(t, err)
		assert.Len(t, r.Items, 2)

		r, err = w.grants.ListGrants(w.ctx, iam.ListGrantsQuery{OrganizationID: w.org, ResourceIDs: []string{w.ws}, GrantID: expired.ID(), IncludeExpired: true})
		require.NoError(t, err)
		require.Len(t, r.Items, 1)
		assert.Equal(t, expired.ID(), r.Items[0].Grant.ID)
	})

	t.Run("counts the active users who own the Organization directly", func(t *testing.T) {
		w.writeGrant(t, iam.RoleOwner, w.org, &linus, nil, nil)
		w.writeGrant(t, iam.RoleOwner, w.org, nil, &deployers, nil)
		w.writeGrant(t, iam.RoleOwner, w.org, &grace, nil, &past)
		n, err := w.grants.CountOwners(w.ctx, w.org, now)
		require.NoError(t, err)
		assert.Equal(t, 1, n, "not a suspended user, a group, or an expired grant")
	})

	t.Run("deletes a grant", func(t *testing.T) {
		require.NoError(t, w.grants.Delete(w.ctx, onWorkspace))
		_, err := w.grants.Get(w.ctx, w.org, onWorkspace.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})
}

func TestResourceRepository(t *testing.T) {
	w := newAccessWorld(t)
	resources := NewResourceRepository(w.db)

	res, err := resources.GetResource(w.ctx, w.org, w.env)
	require.NoError(t, err)
	assert.Equal(t, iam.Resource{ID: w.env, OrganizationID: w.org, Type: iam.ResourceEnvironment, ParentID: w.ws}, res)

	ids, err := resources.ResourceAncestors(w.ctx, w.org, w.env)
	require.NoError(t, err)
	assert.Equal(t, []string{w.env, w.ws, w.org}, ids, "from the resource up to the Organization")

	require.NoError(t, w.resources.ArchiveResource(w.ctx, w.org, w.env))
	res, err = resources.GetResource(w.ctx, w.org, w.env)
	require.NoError(t, err)
	assert.True(t, res.Archived)

	_, err = resources.GetResource(w.ctx, w.org, "env_01JZ0000000000000000000099")
	assert.ErrorIs(t, err, iam.ErrNotFound)
	_, err = resources.ResourceAncestors(w.ctx, w.org, "env_01JZ0000000000000000000099")
	assert.ErrorIs(t, err, iam.ErrNotFound)
	_, err = resources.GetResource(w.ctx, w.writeOrg(t, "globex").ID(), w.env)
	assert.ErrorIs(t, err, iam.ErrNotFound, "not another Organization's")
}
