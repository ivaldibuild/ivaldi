package profile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func TestPermissionsFollowTheCallersRoles(t *testing.T) {
	w := newWorld(t)
	ada := identity.Actor{PrincipalID: w.ada, Kind: identity.ActorUser}
	asked := []string{"source.manage", "source.read", "source.read"}

	w.access.RoleIDs = []string{role(iam.RoleMember)}
	granted, err := w.svc.Permissions(context.Background(), ada, w.org.ID(), "prj_1", asked)
	require.NoError(t, err)
	assert.Equal(t, []string{"source.read"}, granted, "a member reads but does not manage")

	w.access.RoleIDs = []string{role(iam.RoleAdmin)}
	granted, err = w.svc.Permissions(context.Background(), ada, w.org.ID(), "prj_1", asked)
	require.NoError(t, err)
	assert.Equal(t, []string{"source.manage", "source.read"}, granted)

	w.access.ResourceFound = false
	granted, err = w.svc.Permissions(context.Background(), ada, w.org.ID(), "prj_elsewhere", asked)
	require.NoError(t, err)
	assert.Empty(t, granted, "nothing is held on a resource outside the Organization")
}

func TestPermissionsBoundsHowManyAreAsked(t *testing.T) {
	w := newWorld(t)
	many := make([]string, MaxPermissionChecks+1)
	for i := range many {
		many[i] = string(rune('a'+i)) + ".read"
	}
	_, err := w.svc.Permissions(context.Background(), identity.Actor{PrincipalID: w.ada, Kind: identity.ActorUser}, w.org.ID(), "", many)
	assert.ErrorIs(t, err, iam.ErrInvalid)
}
