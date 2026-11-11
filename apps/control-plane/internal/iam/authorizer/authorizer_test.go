package authorizer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

type fakeAccess struct {
	access iam.Access
	err    error
	asked  []iam.AccessQuery
}

func (f *fakeAccess) Access(_ context.Context, q iam.AccessQuery) (iam.Access, error) {
	f.asked = append(f.asked, q)
	return f.access, f.err
}

func role(key string) string {
	id, _ := iam.BuiltInRoleID(key)
	return id
}

func active(roles ...string) iam.Access {
	return iam.Access{OrganizationActive: true, PrincipalStatus: iam.PrincipalActive, ResourceFound: true, RoleIDs: roles}
}

func request(permission string) authorization.Request {
	return authorization.Request{
		Actor:          identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser},
		Permission:     permission,
		OrganizationID: "org_acme",
		ResourceID:     "env_prod",
		Action:         "environment.update",
	}
}

// fakePlatform holds the platform workloads platform requests come from.
type fakePlatform map[string]*iam.Workload

func (f fakePlatform) GetPlatform(_ context.Context, principalID string) (*iam.Workload, error) {
	w, ok := f[principalID]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return w, nil
}

func newAuthorizer(t *testing.T, access *fakeAccess) authorization.Authorizer {
	t.Helper()
	return newAuthorizerWith(t, access, fakePlatform{})
}

func newAuthorizerWith(t *testing.T, access *fakeAccess, platform fakePlatform) authorization.Authorizer {
	t.Helper()
	logger := logging.NewMockLogger()
	logger.On("Debug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	a, err := New(WithAccessRepo(access), WithPlatformWorkloads(platform), WithLogger(logger))
	require.NoError(t, err)
	return a
}

func TestNewRequiresItsDependencies(t *testing.T) {
	_, err := New(WithLogger(logging.NewMockLogger()), WithPlatformWorkloads(fakePlatform{}))
	assert.Error(t, err)
	_, err = New(WithAccessRepo(&fakeAccess{}), WithPlatformWorkloads(fakePlatform{}))
	assert.Error(t, err)
	_, err = New(WithAccessRepo(&fakeAccess{}), WithLogger(logging.NewMockLogger()))
	assert.Error(t, err)
}

func TestBuiltInRoles(t *testing.T) {
	cases := []struct {
		role       string
		permission string
		allowed    bool
	}{
		{iam.RoleOwner, "environment.manage", true},
		{iam.RoleOwner, iam.PermissionOrganizationDelete, true},
		{iam.RoleAdmin, "environment.manage", true},
		{iam.RoleAdmin, iam.PermissionOrganizationDelete, false},
		{iam.RoleAdmin, iam.PermissionOwnerManage, false},
		{iam.RoleMember, "environment.read", true},
		{iam.RoleMember, "environment.manage", false},
		{iam.RoleMember, "session.create", true},
		{iam.RoleViewer, "environment.read", true},
		{iam.RoleViewer, "session.create", false},
	}
	for _, tc := range cases {
		t.Run(tc.role+" "+tc.permission, func(t *testing.T) {
			access := &fakeAccess{access: active(role(tc.role))}
			d, err := newAuthorizer(t, access).Authorize(context.Background(), request(tc.permission))
			require.NoError(t, err)
			assert.Regexp(t, `^dcn_[0-9A-Z]{26}$`, d.ID)
			if tc.allowed {
				assert.Equal(t, authorization.OutcomeAllow, d.Outcome)
			} else {
				assert.Equal(t, authorization.OutcomeDeny, d.Outcome)
				assert.NotEmpty(t, d.Reason)
			}
		})
	}
}

func TestAnyGrantedRoleIsEnough(t *testing.T) {
	access := &fakeAccess{access: active(role(iam.RoleViewer), role(iam.RoleAdmin))}
	d, err := newAuthorizer(t, access).Authorize(context.Background(), request("environment.manage"))
	require.NoError(t, err)
	assert.Equal(t, authorization.OutcomeAllow, d.Outcome)

	q := access.asked[0]
	assert.Equal(t, "org_acme", q.OrganizationID)
	assert.Equal(t, "prn_ada", q.PrincipalID)
	assert.Equal(t, "env_prod", q.ResourceID)
	assert.False(t, q.At.IsZero())
}

func TestDenials(t *testing.T) {
	owner := role(iam.RoleOwner)
	cases := map[string]struct {
		access iam.Access
		edit   func(*authorization.Request)
	}{
		"no grants":             {access: active()},
		"a custom role":         {access: active("rol_01JZ0000000000000000000001")},
		"an inactive org":       {access: iam.Access{PrincipalStatus: iam.PrincipalActive, ResourceFound: true, RoleIDs: []string{owner}}},
		"not a member":          {access: iam.Access{OrganizationActive: true, ResourceFound: true, RoleIDs: []string{owner}}},
		"a suspended principal": {access: iam.Access{OrganizationActive: true, PrincipalStatus: iam.PrincipalSuspended, ResourceFound: true, RoleIDs: []string{owner}}},
		"an unknown resource":   {access: iam.Access{OrganizationActive: true, PrincipalStatus: iam.PrincipalActive, RoleIDs: []string{owner}}},
		"an agent session":      {access: active(owner), edit: func(r *authorization.Request) { r.Actor.Kind = identity.ActorAgentSession }},
		"the system":            {access: active(owner), edit: func(r *authorization.Request) { r.Actor.Kind = identity.ActorSystem }},
		"a claimed approval":    {access: active(owner), edit: func(r *authorization.Request) { r.ApprovalID = "dcn_earlier" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := request("environment.manage")
			if tc.edit != nil {
				tc.edit(&req)
			}
			d, err := newAuthorizer(t, &fakeAccess{access: tc.access}).Authorize(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, authorization.OutcomeDeny, d.Outcome)
			assert.NotEmpty(t, d.Reason)
			assert.NotEmpty(t, d.ID)

			_, err = authorization.Require(context.Background(), newAuthorizer(t, &fakeAccess{access: tc.access}), req)
			assert.ErrorIs(t, err, authorization.ErrDenied)
		})
	}
}

func TestErrors(t *testing.T) {
	t.Run("a malformed request", func(t *testing.T) {
		access := &fakeAccess{access: active(role(iam.RoleOwner))}
		req := request("environment.manage")
		req.ResourceID = ""
		_, err := newAuthorizer(t, access).Authorize(context.Background(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource ID is required")
		assert.Empty(t, access.asked)
	})
	t.Run("reading access fails", func(t *testing.T) {
		boom := errors.New("database down")
		_, err := newAuthorizer(t, &fakeAccess{err: boom}).Authorize(context.Background(), request("environment.read"))
		assert.ErrorIs(t, err, boom)
	})
}
