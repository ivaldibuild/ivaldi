// Package profile tells a signed-in caller who they are: GET /v1/me.
package profile

import (
	"context"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type ProfileServiceOption func(*options)

type options struct {
	principals iam.PrincipalRepository
	users      iam.UserRepository
	accounts   iam.AccountRepository
	orgs       iam.OrganizationRepository
	access     iam.AccessRepository
}

func WithPrincipalRepo(r iam.PrincipalRepository) ProfileServiceOption {
	return func(o *options) { o.principals = r }
}

// WithUserRepo finds a user's Account, for its email.
func WithUserRepo(r iam.UserRepository) ProfileServiceOption {
	return func(o *options) { o.users = r }
}

func WithAccountRepo(r iam.AccountRepository) ProfileServiceOption {
	return func(o *options) { o.accounts = r }
}

func WithOrganizationRepo(r iam.OrganizationRepository) ProfileServiceOption {
	return func(o *options) { o.orgs = r }
}

// WithAccessRepo reads the caller's status and roles, as the authorizer does.
func WithAccessRepo(r iam.AccessRepository) ProfileServiceOption {
	return func(o *options) { o.access = r }
}

// NewProfileService returns the ProfileService. Every option is required.
func NewProfileService(opts ...ProfileServiceOption) (iam.ProfileService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"PrincipalRepo", o.principals != nil},
		{"UserRepo", o.users != nil},
		{"AccountRepo", o.accounts != nil},
		{"OrganizationRepo", o.orgs != nil},
		{"AccessRepo", o.access != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("profile service: %s is required", r.name)
		}
	}
	return &service{options: o}, nil
}

type service struct {
	options
}

func (s *service) Profile(ctx context.Context, actor identity.Actor, organizationID string) (*iam.Profile, error) {
	if organizationID == "" || actor.PrincipalID == "" {
		return nil, iam.NewErrInvalidInput("the caller has no principal or Organization")
	}

	// The Organization is its own resource, so the roles granted on it are
	// the caller's roles at Organization scope.
	access, err := s.access.Access(ctx, iam.AccessQuery{
		OrganizationID: organizationID,
		PrincipalID:    actor.PrincipalID,
		ResourceID:     organizationID,
		At:             clock.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("read access: %w", err)
	}
	switch {
	case !access.OrganizationActive:
		return nil, fmt.Errorf("organization is not active: %w", iam.ErrStatus)
	case access.PrincipalStatus == "":
		return nil, fmt.Errorf("principal: %w", iam.ErrNotFound)
	case access.PrincipalStatus != iam.PrincipalActive:
		return nil, fmt.Errorf("principal is %s: %w", access.PrincipalStatus, iam.ErrStatus)
	}

	principal, err := s.principals.Get(ctx, organizationID, actor.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	org, err := s.orgs.GetByIdentifier(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	p := &iam.Profile{
		Principal:    principal,
		Organization: org.Snapshot(),
		Roles:        roles(organizationID, access.RoleIDs),
	}

	if principal.Kind == iam.PrincipalUser {
		email, err := s.email(ctx, organizationID, actor.PrincipalID)
		if err != nil {
			return nil, err
		}
		p.Email = &email
	}
	return p, nil
}

// email returns the user's Account email, refusing an Account that is not
// active, as signing in does.
func (s *service) email(ctx context.Context, organizationID, principalID string) (string, error) {
	u, err := s.users.Get(ctx, organizationID, principalID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	a, err := s.accounts.Get(ctx, u.AccountID())
	if err != nil {
		return "", fmt.Errorf("get account: %w", err)
	}
	if a.Status() != iam.AccountActive {
		return "", fmt.Errorf("account is %s: %w", a.Status(), iam.ErrStatus)
	}
	return a.Email(), nil
}

func roles(resourceID string, roleIDs []string) []iam.ProfileRole {
	out := make([]iam.ProfileRole, 0, len(roleIDs))
	for _, id := range slices.Compact(slices.Sorted(slices.Values(roleIDs))) {
		key, _ := iam.BuiltInRoleKey(id)
		out = append(out, iam.ProfileRole{Key: key, RoleID: id, ResourceID: resourceID})
	}
	return out
}
