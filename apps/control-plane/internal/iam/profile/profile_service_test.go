package profile

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func user(principalID string) identity.Actor {
	return identity.Actor{PrincipalID: principalID, Kind: identity.ActorUser}
}

func TestNewProfileServiceRequiresEveryRepository(t *testing.T) {
	s := newWorld(t).store
	all := s.options()
	for i := range all {
		without := append(append([]ProfileServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewProfileService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

func TestProfileOfAUser(t *testing.T) {
	w := newWorld(t)

	p, err := w.svc.Profile(context.Background(), user(w.ada), w.org.ID())
	require.NoError(t, err)

	assert.Equal(t, w.ada, p.Principal.ID)
	assert.Equal(t, iam.PrincipalUser, p.Principal.Kind)
	assert.Equal(t, "Ada", p.Principal.DisplayName, "the name the user goes by in this Organization")
	require.NotNil(t, p.Email)
	assert.Equal(t, "ada@example.com", *p.Email)
	assert.Equal(t, "acme", p.Organization.Slug)
	assert.Equal(t, []iam.ProfileRole{
		{Key: iam.RoleOwner, RoleID: role(iam.RoleOwner), ResourceID: w.org.ID()},
		{Key: iam.RoleMember, RoleID: role(iam.RoleMember), ResourceID: w.org.ID()},
	}, p.Roles, "sorted from owner down, each role once")

	require.Len(t, w.asked, 1)
	q := w.asked[0]
	assert.Equal(t, w.org.ID(), q.OrganizationID)
	assert.Equal(t, w.ada, q.PrincipalID)
	assert.Equal(t, w.org.ID(), q.ResourceID, "roles at Organization scope")
	assert.False(t, q.At.IsZero())
}

func TestProfileOfAnAgentHasNoEmail(t *testing.T) {
	w := newWorld(t)
	w.access.RoleIDs = nil

	p, err := w.svc.Profile(context.Background(), identity.Actor{PrincipalID: w.agent, Kind: identity.ActorAgentSession}, w.org.ID())
	require.NoError(t, err)
	assert.Equal(t, iam.PrincipalAgent, p.Principal.Kind)
	assert.Nil(t, p.Email)
	assert.Empty(t, p.Roles)
	assert.NotNil(t, p.Roles, "an empty list, not null")
}

func TestProfileRefusals(t *testing.T) {
	cases := map[string]struct {
		edit func(*world) (identity.Actor, string)
		want error
	}{
		"no Organization": {
			edit: func(w *world) (identity.Actor, string) { return user(w.ada), "" },
			want: iam.ErrInvalid,
		},
		"no principal": {
			edit: func(w *world) (identity.Actor, string) { return identity.Actor{}, w.org.ID() },
			want: iam.ErrInvalid,
		},
		"an inactive Organization": {
			edit: func(w *world) (identity.Actor, string) {
				w.access.OrganizationActive = false
				return user(w.ada), w.org.ID()
			},
			want: iam.ErrStatus,
		},
		"not one of the Organization's principals": {
			edit: func(w *world) (identity.Actor, string) {
				w.access.PrincipalStatus = ""
				return user(w.ada), w.org.ID()
			},
			want: iam.ErrNotFound,
		},
		"a suspended principal": {
			edit: func(w *world) (identity.Actor, string) {
				w.access.PrincipalStatus = iam.PrincipalSuspended
				return user(w.ada), w.org.ID()
			},
			want: iam.ErrStatus,
		},
		"a suspended Account": {
			edit: func(w *world) (identity.Actor, string) {
				for _, a := range w.accounts {
					require.NoError(t, a.Suspend())
				}
				return user(w.ada), w.org.ID()
			},
			want: iam.ErrStatus,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			actor, org := tc.edit(w)
			_, err := w.svc.Profile(context.Background(), actor, org)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestProfileWhenAccessCannotBeRead(t *testing.T) {
	w := newWorld(t)
	boom := errors.New("database down")
	w.accessErr = boom
	_, err := w.svc.Profile(context.Background(), user(w.ada), w.org.ID())
	assert.ErrorIs(t, err, boom)
}
