// Package signin implements iam.Authentication: it checks a person's
// credentials or an identity provider's word for them, links or provisions
// their Account and User, and says which Organizations they may act in.
package signin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Option configures New.
type Option func(*service)

func WithLogger(l logging.Logger) Option { return func(s *service) { s.logger = l } }
func WithOrganizationRepo(r iam.OrganizationRepository) Option {
	return func(s *service) { s.orgs = r }
}
func WithAccountRepo(r iam.AccountRepository) Option { return func(s *service) { s.accounts = r } }
func WithPasswordCredentialRepo(r iam.PasswordCredentialRepository) Option {
	return func(s *service) { s.passwords = r }
}
func WithUserRepo(r iam.UserRepository) Option { return func(s *service) { s.users = r } }
func WithIdentityProviderRepo(r iam.IdentityProviderRepository) Option {
	return func(s *service) { s.providers = r }
}
func WithExternalIdentityRepo(r iam.ExternalIdentityRepository) Option {
	return func(s *service) { s.externals = r }
}
func WithGrantRepo(r iam.GrantRepository) Option        { return func(s *service) { s.grants = r } }
func WithPasswordHasher(h hasher.PasswordHasher) Option { return func(s *service) { s.hasher = h } }
func WithTransactor(tx transaction.Transactor) Option   { return func(s *service) { s.tx = tx } }
func WithOutboxRecorder(o outbox.Recorder) Option       { return func(s *service) { s.outbox = o } }

// WithBuiltinAccounts allows signing in with an email and password.
func WithBuiltinAccounts(on bool) Option { return func(s *service) { s.builtin = on } }

// WithBootstrapAdminEmail names the self-hosted administrator, who becomes
// the Organization's owner on first signing in through one of its configured
// providers, while it has none.
func WithBootstrapAdminEmail(email string) Option {
	return func(s *service) { s.adminEmail = email }
}

type service struct {
	logger     logging.Logger
	orgs       iam.OrganizationRepository
	accounts   iam.AccountRepository
	passwords  iam.PasswordCredentialRepository
	users      iam.UserRepository
	providers  iam.IdentityProviderRepository
	externals  iam.ExternalIdentityRepository
	grants     iam.GrantRepository
	hasher     hasher.PasswordHasher
	tx         transaction.Transactor
	outbox     outbox.Recorder
	builtin    bool
	adminEmail string
}

// New returns the Authentication service, or an error naming a missing
// dependency.
func New(opts ...Option) (iam.Authentication, error) {
	s := &service{}
	for _, o := range opts {
		o(s)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Logger", s.logger != nil},
		{"OrganizationRepo", s.orgs != nil},
		{"AccountRepo", s.accounts != nil},
		{"PasswordCredentialRepo", s.passwords != nil},
		{"UserRepo", s.users != nil},
		{"IdentityProviderRepo", s.providers != nil},
		{"ExternalIdentityRepo", s.externals != nil},
		{"GrantRepo", s.grants != nil},
		{"PasswordHasher", s.hasher != nil},
		{"Transactor", s.tx != nil},
		{"Outbox", s.outbox != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("signin: %s is required", r.name)
		}
	}
	return s, nil
}

func (s *service) AuthenticatePassword(ctx context.Context, email, password string) (*iam.SignedIn, error) {
	if !s.builtin {
		return nil, fmt.Errorf("%w: built-in accounts are off", iam.ErrNotAdmitted)
	}
	account, err := s.accounts.GetByEmail(ctx, email)
	if errors.Is(err, iam.ErrNotFound) {
		s.hasher.VerifyDummy(password)
		return nil, iam.ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	cred, err := s.passwords.Get(ctx, account.ID())
	if errors.Is(err, iam.ErrNotFound) {
		s.hasher.VerifyDummy(password)
		return nil, iam.ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := s.hasher.Verify(password, cred.PasswordHash())
	if err != nil {
		return nil, fmt.Errorf("check password of account %s: %w", account.ID(), err)
	}
	if !ok {
		return nil, iam.ErrInvalidCredentials
	}
	if !account.Active() {
		return nil, fmt.Errorf("%w: account %s is %s", iam.ErrStatus, account.ID(), account.Status())
	}
	memberships, err := s.activeMemberships(ctx, account.ID(), "")
	if err != nil {
		return nil, err
	}
	return &iam.SignedIn{AccountID: account.ID(), Method: iam.SignInPassword, Memberships: memberships}, nil
}

func (s *service) AuthenticateExternal(ctx context.Context, organizationID, identityProviderID string, profile iam.ExternalProfile) (*iam.SignedIn, error) {
	idp, err := s.providers.Get(ctx, organizationID, identityProviderID)
	if err != nil {
		return nil, err
	}
	if !idp.Active() {
		return nil, fmt.Errorf("%w: %s is disabled", iam.ErrNotAdmitted, idp.Name())
	}
	if err := idp.Admits(profile); err != nil {
		return nil, err
	}

	var accountID string
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		account, user, err := s.linkExternal(ctx, idp, profile)
		if err != nil {
			return err
		}
		accountID = account.ID()
		return s.claimOwnership(ctx, idp, profile, user)
	})
	if err != nil {
		return nil, err
	}
	memberships, err := s.activeMemberships(ctx, accountID, organizationID)
	if err != nil {
		return nil, err
	}
	return &iam.SignedIn{
		AccountID:          accountID,
		Method:             iam.SignInIdentityProvider,
		IdentityProviderID: identityProviderID,
		RestrictedTo:       organizationID,
		Memberships:        memberships,
	}, nil
}

// linkExternal finds the Account and User the provider's subject signs in
// as, linking or provisioning them the first time.
func (s *service) linkExternal(ctx context.Context, idp *iam.IdentityProvider, profile iam.ExternalProfile) (*iam.Account, *iam.User, error) {
	orgID := idp.OrganizationID()
	x, err := s.externals.GetBySubject(ctx, orgID, idp.ID(), profile.Subject)
	switch {
	case err == nil:
		user, err := s.users.Get(ctx, orgID, x.PrincipalID())
		if err != nil {
			return nil, nil, err
		}
		account, err := s.accounts.Get(ctx, user.AccountID())
		if err != nil {
			return nil, nil, err
		}
		if err := checkActive(account, user); err != nil {
			return nil, nil, err
		}
		x.RecordLogin()
		if err := s.externals.Write(ctx, x); err != nil {
			return nil, nil, err
		}
		return account, user, nil
	case !errors.Is(err, iam.ErrNotFound):
		return nil, nil, err
	}

	// A first sign-in through this provider: its email is all that can tie
	// it to an existing Account, so it must be verified.
	if profile.Email == "" || !profile.EmailVerified {
		return nil, nil, fmt.Errorf("%w: %s gave no verified email", iam.ErrNotAdmitted, idp.Name())
	}
	provisions := idp.JITProvisioning() || s.isAdmin(idp, profile)
	var raised []source

	account, err := s.accounts.GetByEmail(ctx, profile.Email)
	switch {
	case errors.Is(err, iam.ErrNotFound):
		if !provisions {
			return nil, nil, fmt.Errorf("%w: %s does not provision new members", iam.ErrNoMembership, idp.Name())
		}
		account, err = iam.NewAccount(iam.NewAccountProps{Email: profile.Email, DisplayName: displayName(profile)}, nil)
		if err != nil {
			return nil, nil, err
		}
		account.VerifyEmail()
		if err := s.accounts.Write(ctx, account); err != nil {
			return nil, nil, err
		}
		raised = append(raised, source{iam.AggregateAccount, account.ID(), account})
	case err != nil:
		return nil, nil, err
	default:
		// Only a provider the installation's operator configured may vouch
		// for an existing Account; any other needs proof, such as a verified
		// domain or an invitation, before taking over someone's account.
		if !idp.ManagedByConfiguration() {
			return nil, nil, fmt.Errorf("%w: sign in another way to link %s", iam.ErrAccountExists, idp.Name())
		}
		if !account.Active() {
			return nil, nil, fmt.Errorf("%w: account %s is %s", iam.ErrStatus, account.ID(), account.Status())
		}
	}

	user, err := s.users.GetByAccount(ctx, idp.OrganizationID(), account.ID())
	switch {
	case errors.Is(err, iam.ErrNotFound):
		if !provisions {
			return nil, nil, fmt.Errorf("%w: %s does not provision new members", iam.ErrNoMembership, idp.Name())
		}
		user, err = iam.NewUser(iam.NewUserProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: idp.OrganizationID(), DisplayName: displayName(profile)},
			AccountID:      account.ID(),
		}, nil)
		if err != nil {
			return nil, nil, err
		}
		if err := s.users.Write(ctx, user); err != nil {
			return nil, nil, err
		}
		raised = append(raised, source{iam.AggregateUser, user.ID(), user})
	case err != nil:
		return nil, nil, err
	default:
		if err := checkActive(account, user); err != nil {
			return nil, nil, err
		}
	}

	x, err = iam.NewExternalIdentity(iam.NewExternalIdentityProps{
		OrganizationID:     idp.OrganizationID(),
		PrincipalID:        user.ID(),
		IdentityProviderID: idp.ID(),
		Subject:            profile.Subject,
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	x.RecordLogin()
	if err := s.externals.Write(ctx, x); err != nil {
		return nil, nil, err
	}
	raised = append(raised, source{iam.AggregateExternalIdentity, x.ID(), x})
	if err := s.record(ctx, idp.OrganizationID(), raised); err != nil {
		return nil, nil, err
	}
	return account, user, nil
}

// claimOwnership makes the bootstrap administrator the Organization's owner
// the first time they sign in through one of its configured providers. The
// Organization's row stays locked until the transaction ends, so of two
// administrators signing in at once only one becomes owner, and once anyone
// owns it this does nothing.
func (s *service) claimOwnership(ctx context.Context, idp *iam.IdentityProvider, profile iam.ExternalProfile, user *iam.User) error {
	if !s.isAdmin(idp, profile) {
		return nil
	}
	org, err := s.orgs.GetForUpdate(ctx, idp.OrganizationID())
	if err != nil {
		return err
	}
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	owned, err := s.grants.ExistsForRole(ctx, org.ID(), ownerRole, org.ID())
	if err != nil || owned {
		return err
	}
	grant, err := iam.NewOwnerGrant(org.ID(), user.ID())
	if err != nil {
		return err
	}
	if err := s.grants.Write(ctx, grant); err != nil {
		return err
	}
	s.logger.Info("bootstrap administrator claimed the organization", logging.F("organization_id", org.ID()), logging.F("principal_id", user.ID()))
	return s.record(ctx, org.ID(), []source{{iam.AggregateGrant, grant.ID(), grant}})
}

// isAdmin reports whether the sign-in is the bootstrap administrator's: the
// configured email, verified by a provider the configuration manages.
func (s *service) isAdmin(idp *iam.IdentityProvider, profile iam.ExternalProfile) bool {
	return s.adminEmail != "" && idp.ManagedByConfiguration() && profile.EmailVerified &&
		strings.EqualFold(profile.Email, s.adminEmail)
}

func (s *service) Membership(ctx context.Context, accountID, organizationID string) (*iam.Membership, error) {
	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !account.Active() {
		return nil, fmt.Errorf("%w: account %s is %s", iam.ErrNoMembership, accountID, account.Status())
	}
	memberships, err := s.activeMemberships(ctx, accountID, organizationID)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, iam.ErrNoMembership
	}
	return &memberships[0], nil
}

// activeMemberships returns the Account's active memberships, in the one
// Organization when organizationID is set.
func (s *service) activeMemberships(ctx context.Context, accountID, organizationID string) ([]iam.Membership, error) {
	all, err := s.users.ListMemberships(ctx, accountID)
	if err != nil {
		return nil, err
	}
	var active []iam.Membership
	for _, m := range all {
		if m.Active() && (organizationID == "" || m.OrganizationID == organizationID) {
			active = append(active, m)
		}
	}
	return active, nil
}

func checkActive(account *iam.Account, user *iam.User) error {
	if !account.Active() {
		return fmt.Errorf("%w: account %s is %s", iam.ErrStatus, account.ID(), account.Status())
	}
	if !user.Active() {
		return fmt.Errorf("%w: membership %s is %s", iam.ErrStatus, user.ID(), user.Status())
	}
	return nil
}

// displayName is the name a provider gave, or the email's local part.
func displayName(p iam.ExternalProfile) string {
	if name := strings.TrimSpace(p.DisplayName); name != "" {
		return name
	}
	local, _, _ := strings.Cut(p.Email, "@")
	return local
}

type source struct {
	aggregateType string
	aggregateID   string
	obj           interface{ PullEvents() []aggregate.Event }
}

func (s *service) record(ctx context.Context, organizationID string, sources []source) error {
	for _, src := range sources {
		envelopes, err := events.Wrap(ctx, events.Source{OrganizationID: organizationID, AggregateType: src.aggregateType, AggregateID: src.aggregateID}, src.obj.PullEvents()...)
		if err != nil {
			return fmt.Errorf("wrap %s events: %w", src.aggregateType, err)
		}
		if err := s.outbox.Record(ctx, envelopes...); err != nil {
			return fmt.Errorf("record %s events: %w", src.aggregateType, err)
		}
	}
	return nil
}
