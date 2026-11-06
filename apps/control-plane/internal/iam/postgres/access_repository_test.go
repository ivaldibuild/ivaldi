package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// accessWorld is an Organization with a Workspace and an Environment under
// it, and a member, Ada.
type accessWorld struct {
	*fixture
	org, ws, env, ada string
	access            iam.AccessRepository
}

func newAccessWorld(t *testing.T) *accessWorld {
	t.Helper()
	f := newFixture(t)
	o := f.writeOrg(t, "acme")
	u := newUser(o.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, f.users.Write(f.ctx, u))
	w := &accessWorld{
		fixture: f, org: o.ID(), ada: u.ID(),
		ws:     "wsp_" + id.ULID(),
		env:    "env_" + id.ULID(),
		access: NewAccessRepository(f.db),
	}
	for _, r := range []iam.Resource{
		{ID: w.org, OrganizationID: w.org, Type: iam.ResourceOrganization},
		{ID: w.ws, OrganizationID: w.org, Type: iam.ResourceWorkspace, ParentID: w.org},
		{ID: w.env, OrganizationID: w.org, Type: iam.ResourceEnvironment, ParentID: w.ws},
	} {
		require.NoError(t, f.resources.RegisterResource(f.ctx, r))
	}
	return w
}

func (w *accessWorld) grant(t *testing.T, role, resourceID string, principalID, groupID *string, expires *time.Time) {
	t.Helper()
	roleID, _ := iam.BuiltInRoleID(role)
	// A grant that has expired by t0 was made a day before it expired.
	created := t0.Add(-48 * time.Hour)
	g := must(iam.NewGrant(iam.NewGrantProps{
		OrganizationID: w.org, PrincipalID: principalID, GroupID: groupID, RoleID: roleID,
		ResourceID: resourceID, GrantedByPrincipalID: w.ada, ExpiresAt: expires, CreatedAt: &created,
	}, nil))
	require.NoError(t, w.grants.Write(w.ctx, g))
}

func (w *accessWorld) group(t *testing.T, name string, members ...string) string {
	t.Helper()
	gid := id.New("grp")
	require.NoError(t, w.db.Exec(`INSERT INTO iam.groups (id, organization_id, name, source) VALUES (?, ?, ?, 'manual')`, gid, w.org, name).Error)
	for _, m := range members {
		require.NoError(t, w.db.Exec(`INSERT INTO iam.group_members (organization_id, group_id, principal_id) VALUES (?, ?, ?)`, w.org, gid, m).Error)
	}
	return gid
}

func (w *accessWorld) check(t *testing.T, resourceID string) iam.Access {
	t.Helper()
	a, err := w.access.Access(w.ctx, iam.AccessQuery{OrganizationID: w.org, PrincipalID: w.ada, ResourceID: resourceID, At: t0})
	require.NoError(t, err)
	return a
}

func roleID(key string) string {
	id, _ := iam.BuiltInRoleID(key)
	return id
}

func TestAccessCollectsGrantsOnAncestors(t *testing.T) {
	w := newAccessWorld(t)
	w.grant(t, iam.RoleViewer, w.org, &w.ada, nil, nil)
	w.grant(t, iam.RoleAdmin, w.ws, &w.ada, nil, nil)

	a := w.check(t, w.env)

	assert.True(t, a.OrganizationActive)
	assert.Equal(t, iam.PrincipalActive, a.PrincipalStatus)
	assert.True(t, a.ResourceFound)
	assert.ElementsMatch(t, []string{roleID(iam.RoleViewer), roleID(iam.RoleAdmin)}, a.RoleIDs)

	assert.Equal(t, []string{roleID(iam.RoleViewer)}, w.check(t, w.org).RoleIDs, "a grant does not reach up the hierarchy")
}

func TestAccessThroughGroups(t *testing.T) {
	w := newAccessWorld(t)
	gid := w.group(t, "deployers", w.ada)
	other := w.group(t, "others")
	w.grant(t, iam.RoleMember, w.env, nil, &gid, nil)
	w.grant(t, iam.RoleAdmin, w.env, nil, &other, nil)

	assert.Equal(t, []string{roleID(iam.RoleMember)}, w.check(t, w.env).RoleIDs)
}

func TestAccessIgnoresExpiredGrantsAndArchivedResources(t *testing.T) {
	w := newAccessWorld(t)
	past := t0.Add(-time.Minute)
	future := t0.Add(time.Hour)
	w.grant(t, iam.RoleAdmin, w.env, &w.ada, nil, &past)
	w.grant(t, iam.RoleMember, w.env, &w.ada, nil, &future)
	assert.Equal(t, []string{roleID(iam.RoleMember)}, w.check(t, w.env).RoleIDs)

	w.grant(t, iam.RoleViewer, w.ws, &w.ada, nil, nil)
	require.NoError(t, w.resources.ArchiveResource(w.ctx, w.org, w.env))
	a := w.check(t, w.env)
	assert.True(t, a.ResourceFound, "an archived resource is still found")
	assert.Equal(t, []string{roleID(iam.RoleViewer)}, a.RoleIDs, "grants on the archived resource stop; its ancestors' apply")
}

func TestAccessUnknowns(t *testing.T) {
	w := newAccessWorld(t)
	w.grant(t, iam.RoleOwner, w.org, &w.ada, nil, nil)

	a := w.check(t, "env_"+id.ULID())
	assert.False(t, a.ResourceFound)
	assert.Empty(t, a.RoleIDs)

	a, err := w.access.Access(w.ctx, iam.AccessQuery{OrganizationID: w.org, PrincipalID: id.New("prn"), ResourceID: w.env, At: t0})
	require.NoError(t, err)
	assert.Empty(t, a.PrincipalStatus)
	assert.Empty(t, a.RoleIDs)

	// Another Organization's view of the same IDs sees nothing.
	other := w.writeOrg(t, "globex")
	a, err = w.access.Access(w.ctx, iam.AccessQuery{OrganizationID: other.ID(), PrincipalID: w.ada, ResourceID: w.env, At: t0})
	require.NoError(t, err)
	assert.True(t, a.OrganizationActive)
	assert.Empty(t, a.PrincipalStatus)
	assert.False(t, a.ResourceFound)
	assert.Empty(t, a.RoleIDs)
}
