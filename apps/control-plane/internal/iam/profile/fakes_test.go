package profile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authorizer"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// store holds what the fake repositories below read. Each fake embeds its
// repository interface, so a call to anything but the method it defines
// panics.
type store struct {
	org        *iam.Organization
	principals map[string]iam.PrincipalSnapshot
	users      map[string]*iam.User
	accounts   map[string]*iam.Account
	access     iam.Access
	accessErr  error
	asked      []iam.AccessQuery
}

type principals struct{ *store }

func (p principals) Get(_ context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error) {
	s, ok := p.principals[principalID]
	if !ok || s.OrganizationID != organizationID {
		return iam.PrincipalSnapshot{}, iam.ErrNotFound
	}
	return s, nil
}

type users struct {
	iam.UserRepository
	*store
}

func (u users) Get(_ context.Context, organizationID, principalID string) (*iam.User, error) {
	user, ok := u.users[principalID]
	if !ok || user.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return user, nil
}

type accounts struct {
	iam.AccountRepository
	*store
}

func (a accounts) Get(_ context.Context, accountID string) (*iam.Account, error) {
	acct, ok := a.accounts[accountID]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return acct, nil
}

type orgs struct {
	iam.OrganizationRepository
	*store
}

func (o orgs) GetByIdentifier(_ context.Context, identifier string) (*iam.Organization, error) {
	if o.org == nil || (o.org.ID() != identifier && o.org.Slug() != identifier) {
		return nil, iam.ErrNotFound
	}
	return o.org, nil
}

type access struct{ *store }

func (a access) Access(_ context.Context, q iam.AccessQuery) (iam.Access, error) {
	a.asked = append(a.asked, q)
	return a.access, a.accessErr
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// world is the Acme Organization with Ada, a user, and Release Bot, an
// Agent.
type world struct {
	*store
	svc        iam.ProfileService
	ada, agent string
}

func role(key string) string {
	id, _ := iam.BuiltInRoleID(key)
	return id
}

func newWorld(t *testing.T) *world {
	t.Helper()
	org := must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, nil))
	acct := must(iam.NewAccount(iam.NewAccountProps{Email: "ada@example.com", DisplayName: "Ada Lovelace"}, nil))
	ada := must(iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Ada"},
		AccountID:      acct.ID(),
	}, nil))
	agent := iam.PrincipalSnapshot{ID: id.New("prn"), OrganizationID: org.ID(), Kind: iam.PrincipalAgent, DisplayName: "Release Bot", Status: iam.PrincipalActive}

	s := &store{
		org:        org,
		principals: map[string]iam.PrincipalSnapshot{ada.ID(): ada.Snapshot().PrincipalSnapshot, agent.ID: agent},
		users:      map[string]*iam.User{ada.ID(): ada},
		accounts:   map[string]*iam.Account{acct.ID(): acct},
		access: iam.Access{
			OrganizationActive: true, PrincipalStatus: iam.PrincipalActive, ResourceFound: true,
			RoleIDs: []string{role(iam.RoleMember), role(iam.RoleOwner), role(iam.RoleMember)},
		},
	}
	svc, err := NewProfileService(s.options()...)
	require.NoError(t, err)
	return &world{store: s, svc: svc, ada: ada.ID(), agent: agent.ID}
}

func (s *store) options() []ProfileServiceOption {
	return []ProfileServiceOption{
		WithPrincipalRepo(principals{s}), WithUserRepo(users{store: s}), WithAccountRepo(accounts{store: s}),
		WithOrganizationRepo(orgs{store: s}), WithAccessRepo(access{s}),
		WithAuthorizer(s.authorizer()),
	}
}

// authorizer is IAM's own authorizer over the fake access, so permission
// checks follow the real role rules.
func (s *store) authorizer() authorization.Authorizer {
	logger := logging.NewMockLogger()
	// A denial logs its message and six fields.
	denied := []any{mock.Anything}
	for range 6 {
		denied = append(denied, mock.Anything)
	}
	logger.On("Debug", denied...).Maybe()
	return must(authorizer.New(authorizer.WithAccessRepo(access{s}),
		authorizer.WithPlatformWorkloads(noPlatformWorkloads{}), authorizer.WithAgentSessions(noAgentSessions{}),
		authorizer.WithResourceAncestry(noAgentSessions{}), authorizer.WithLogger(logger)))
}

// noAgentSessions is an Organization where no agent Session acts.
type noAgentSessions struct{}

func (noAgentSessions) Get(context.Context, string, string) (*iam.AgentSession, error) {
	return nil, iam.ErrNotFound
}

func (noAgentSessions) ResourceAncestors(_ context.Context, _, resourceID string) ([]string, error) {
	return []string{resourceID}, nil
}

// noPlatformWorkloads is a deployment with no platform workloads.
type noPlatformWorkloads struct{}

func (noPlatformWorkloads) GetPlatform(context.Context, string) (*iam.Workload, error) {
	return nil, iam.ErrNotFound
}
