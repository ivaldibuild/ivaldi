package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestContactReader(t *testing.T) {
	w := newAccessWorld(t)
	addUser := func(email string) string {
		u := newUser(w.org, w.writeAccount(t, email).ID())
		require.NoError(t, w.users.Write(w.ctx, u))
		return u.ID()
	}
	grace, bob := addUser("grace@example.com"), addUser("bob@example.com")
	w.grant(t, iam.RoleOwner, w.org, &w.ada, nil, nil)
	admins := w.group(t, "admins", grace)
	w.grant(t, iam.RoleAdmin, w.org, nil, &admins, nil)
	w.grant(t, iam.RoleMember, w.org, &bob, nil, nil)
	w.grant(t, iam.RoleAdmin, w.ws, &bob, nil, nil)
	contacts := NewContactReader(w.db)

	emails := func(cs []iam.Contact) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.Email
		}
		return out
	}
	got, err := contacts.Administrators(w.ctx, w.org)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ada@example.com", "grace@example.com"}, emails(got),
		"owners and admins of the Organization, directly or through a group; an admin of a Workspace is not one")

	got, err = contacts.ActiveUsers(w.ctx, w.org, []string{w.ada, bob, id.New("prn")})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ada@example.com", "bob@example.com"}, emails(got), "an unknown principal is left out")
}
