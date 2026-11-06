package onboarding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher/hashertest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

var errBoom = errors.New("boom")

// txKey marks a context that fakeTx passed in, so fakes can tell whether a
// write happened inside the transaction.
type txKey struct{}

func inTx(ctx context.Context) bool { return ctx.Value(txKey{}) != nil }

type fakeTx struct{ calls int }

func (f *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(context.WithValue(ctx, txKey{}, true))
}

// write records one repository write.
type write struct {
	obj  any
	inTx bool
}

type fakeOrgs struct {
	names, slugs map[string]bool
	writes       []write
	err          error
}

func (f *fakeOrgs) ExistsByName(_ context.Context, name string) (bool, error) {
	return f.names[strings.ToLower(name)], nil
}
func (f *fakeOrgs) ExistsBySlug(_ context.Context, slug string) (bool, error) {
	return f.slugs[slug], nil
}
func (f *fakeOrgs) GetByIdentifier(context.Context, string) (*iam.Organization, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeOrgs) GetByIdentifiers(context.Context, []string) ([]*iam.Organization, error) {
	return nil, nil
}
func (f *fakeOrgs) Count(context.Context) (int64, error) { return 0, nil }
func (f *fakeOrgs) Oldest(context.Context) (*iam.Organization, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeOrgs) GetForUpdate(context.Context, string) (*iam.Organization, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeOrgs) Write(ctx context.Context, o *iam.Organization) error {
	f.writes = append(f.writes, write{o, inTx(ctx)})
	return f.err
}

type fakeAccounts struct {
	emails map[string]bool
	writes []write
	err    error
}

func (f *fakeAccounts) ExistsByEmail(_ context.Context, email string) (bool, error) {
	return f.emails[strings.ToLower(email)], nil
}
func (f *fakeAccounts) Get(context.Context, string) (*iam.Account, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeAccounts) GetByEmail(context.Context, string) (*iam.Account, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeAccounts) Write(ctx context.Context, a *iam.Account) error {
	f.writes = append(f.writes, write{a, inTx(ctx)})
	return f.err
}

type fakePasswords struct {
	writes []write
	err    error
}

func (f *fakePasswords) Get(context.Context, string) (*iam.PasswordCredential, error) {
	return nil, iam.ErrNotFound
}
func (f *fakePasswords) Write(ctx context.Context, c *iam.PasswordCredential) error {
	f.writes = append(f.writes, write{c, inTx(ctx)})
	return f.err
}

type fakeUsers struct {
	writes []write
	err    error
}

func (f *fakeUsers) Get(context.Context, string, string) (*iam.User, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeUsers) GetByAccount(context.Context, string, string) (*iam.User, error) {
	return nil, iam.ErrNotFound
}
func (f *fakeUsers) ListMemberships(context.Context, string) ([]iam.Membership, error) {
	return nil, nil
}
func (f *fakeUsers) Write(ctx context.Context, u *iam.User) error {
	f.writes = append(f.writes, write{u, inTx(ctx)})
	return f.err
}

type fakeRegistrar struct {
	writes []write
	err    error
}

func (f *fakeRegistrar) RegisterResource(ctx context.Context, r iam.Resource) error {
	f.writes = append(f.writes, write{r, inTx(ctx)})
	return f.err
}
func (f *fakeRegistrar) ArchiveResource(context.Context, string, string) error { return nil }
func (f *fakeRegistrar) RestoreResource(context.Context, string, string) error { return nil }

// fakeProvisioner records the Organizations it provisions.
type fakeProvisioner struct {
	writes []write
	err    error
}

func (f *fakeProvisioner) ProvisionOrganization(ctx context.Context, organizationID string) error {
	f.writes = append(f.writes, write{organizationID, inTx(ctx)})
	return f.err
}

// fakeGrants embeds the repository, so a call to a method it does not fake
// panics.
type fakeGrants struct {
	iam.GrantRepository
	writes []write
	err    error
}

func (f *fakeGrants) ExistsForRole(context.Context, string, string, string) (bool, error) {
	return len(f.writes) > 0, nil
}
func (f *fakeGrants) Write(ctx context.Context, g *iam.Grant) error {
	f.writes = append(f.writes, write{g, inTx(ctx)})
	return f.err
}

type fakePlatform struct {
	writes []write
	err    error
}

func (f *fakePlatform) GetBySubject(context.Context, iam.PlatformProvider, string) (*iam.AccountIdentity, error) {
	return nil, iam.ErrNotFound
}
func (f *fakePlatform) Write(ctx context.Context, a *iam.AccountIdentity) error {
	f.writes = append(f.writes, write{a, inTx(ctx)})
	return f.err
}

type fakeOutbox struct {
	envelopes []events.Envelope
	inTx      bool
	err       error
}

func (f *fakeOutbox) Record(ctx context.Context, envelopes ...events.Envelope) error {
	f.envelopes = append(f.envelopes, envelopes...)
	f.inTx = inTx(ctx)
	return f.err
}

type fixture struct {
	svc       iam.Onboarding
	logger    *logging.MockLogger
	orgs      *fakeOrgs
	accounts  *fakeAccounts
	passwords *fakePasswords
	users     *fakeUsers
	tx        *fakeTx
	outbox    *fakeOutbox
	hasher    *hashertest.Fake
	resources *fakeRegistrar
	grants    *fakeGrants
	platform  *fakePlatform
	provision *fakeProvisioner
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		logger:    logging.NewMockLogger(),
		orgs:      &fakeOrgs{names: map[string]bool{}, slugs: map[string]bool{}},
		accounts:  &fakeAccounts{emails: map[string]bool{}},
		passwords: &fakePasswords{},
		users:     &fakeUsers{},
		tx:        &fakeTx{},
		outbox:    &fakeOutbox{},
		hasher:    hashertest.NewFake(),
		resources: &fakeRegistrar{},
		grants:    &fakeGrants{},
		platform:  &fakePlatform{},
		provision: &fakeProvisioner{},
	}
	svc, err := NewOnboardingService(f.options()...)
	require.NoError(t, err)
	f.svc = svc
	return f
}

func (f *fixture) options() []OnboardingServiceOption {
	return []OnboardingServiceOption{
		WithLogger(f.logger),
		WithOrganizationRepo(f.orgs),
		WithAccountRepo(f.accounts),
		WithPasswordCredentialRepo(f.passwords),
		WithUserRepo(f.users),
		WithTransactor(f.tx),
		WithOutboxRecorder(f.outbox),
		WithPasswordHasher(f.hasher),
		WithResourceRegistrar(f.resources),
		WithGrantRepo(f.grants),
		WithAccountIdentityRepo(f.platform),
		WithOrganizationProvisioner(f.provision),
	}
}

func input() *iam.OnboardOrganizationInput {
	return &iam.OnboardOrganizationInput{
		OwnerEmail:              "ada@example.com",
		OwnerDisplayName:        "Ada Lovelace",
		OwnerPassword:           "correct-horse",
		OrganizationName:        "Analytical Engines Ltd",
		OrganizationDisplayName: "Analytical Engines",
	}
}

func TestNewOnboardingServiceRequiresEveryDependency(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name  string
		unset OnboardingServiceOption
	}{
		{"Logger", WithLogger(nil)},
		{"OrganizationRepo", WithOrganizationRepo(nil)},
		{"AccountRepo", WithAccountRepo(nil)},
		{"PasswordCredentialRepo", WithPasswordCredentialRepo(nil)},
		{"UserRepo", WithUserRepo(nil)},
		{"Transactor", WithTransactor(nil)},
		{"Outbox", WithOutboxRecorder(nil)},
		{"PasswordHasher", WithPasswordHasher(nil)},
		{"ResourceRegistrar", WithResourceRegistrar(nil)},
		{"GrantRepo", WithGrantRepo(nil)},
		{"AccountIdentityRepo", WithAccountIdentityRepo(nil)},
		{"OrganizationProvisioner", WithOrganizationProvisioner(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewOnboardingService(append(f.options(), tt.unset)...)
			require.ErrorContains(t, err, tt.name+" is required")
			assert.Nil(t, svc)
		})
	}
}

func TestOnboardOrganization(t *testing.T) {
	f := newFixture(t)

	require.NoError(t, f.svc.OnboardOrganization(context.Background(), input()))

	assert.Equal(t, 1, f.tx.calls, "one transaction")
	for name, writes := range map[string][]write{"organization": f.orgs.writes, "account": f.accounts.writes, "password": f.passwords.writes, "user": f.users.writes} {
		require.Len(t, writes, 1, name)
		assert.True(t, writes[0].inTx, "%s is written in the transaction", name)
	}

	org := f.orgs.writes[0].obj.(*iam.Organization)
	assert.Equal(t, "Analytical Engines Ltd", org.Name())
	assert.Equal(t, "Analytical Engines", org.DisplayName())
	assert.Equal(t, "analytical-engines", org.Slug(), "derived from the display name")

	account := f.accounts.writes[0].obj.(*iam.Account)
	assert.Equal(t, "ada@example.com", account.Email())

	cred := f.passwords.writes[0].obj.(*iam.PasswordCredential)
	assert.Equal(t, account.ID(), cred.AccountID())
	ok, err := f.hasher.Verify("correct-horse", cred.PasswordHash())
	require.NoError(t, err)
	assert.True(t, ok, "the stored hash is of the owner's password")

	user := f.users.writes[0].obj.(*iam.User)
	assert.Equal(t, org.ID(), user.OrganizationID())
	assert.Equal(t, account.ID(), user.AccountID())
	assert.Equal(t, "Ada Lovelace", user.DisplayName())

	require.Len(t, f.resources.writes, 1)
	assert.True(t, f.resources.writes[0].inTx)
	assert.Equal(t, org.Resource(), f.resources.writes[0].obj, "the Organization is the root of its resource hierarchy")

	require.Len(t, f.provision.writes, 1)
	assert.Equal(t, org.ID(), f.provision.writes[0].obj, "the Organization is provisioned, such as with its default Workspace")
	assert.True(t, f.provision.writes[0].inTx)

	require.Len(t, f.grants.writes, 1)
	assert.True(t, f.grants.writes[0].inTx)
	grant := f.grants.writes[0].obj.(*iam.Grant)
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	assert.Equal(t, ownerRole, grant.RoleID())
	assert.Equal(t, org.ID(), grant.ResourceID())
	assert.Equal(t, user.ID(), *grant.PrincipalID(), "the founder owns the Organization")
	assert.Equal(t, user.ID(), grant.GrantedByPrincipalID())

	assert.True(t, f.outbox.inTx, "events are recorded in the transaction")
	var got []string
	for _, e := range f.outbox.envelopes {
		got = append(got, e.AggregateType+" "+e.Type)
		assert.Equal(t, org.ID(), e.OrganizationID, "every event belongs to the onboarded organization")
	}
	assert.Equal(t, []string{
		"iam.Organization iam.v1.organization.created",
		"iam.Account iam.v1.account.created",
		"iam.PasswordCredential iam.v1.password_credential.created",
		"iam.User iam.v1.user.created",
		"iam.Grant iam.v1.grant.created",
	}, got)
}

func TestProvisionInstallation(t *testing.T) {
	f := newFixture(t)

	org, err := f.svc.ProvisionInstallation(context.Background(), iam.ProvisionInstallationInput{Name: "Acme Corporation", DisplayName: "Acme"})

	require.NoError(t, err)
	assert.Equal(t, "acme", org.Slug())
	assert.Equal(t, "Acme Corporation", org.Name())
	require.Len(t, f.orgs.writes, 1)
	assert.True(t, f.orgs.writes[0].inTx)
	require.Len(t, f.resources.writes, 1)
	assert.Equal(t, org.Resource(), f.resources.writes[0].obj)
	assert.Empty(t, f.grants.writes, "the administrator claims the Organization at first sign-in")
	assert.Empty(t, f.accounts.writes)
	require.Len(t, f.outbox.envelopes, 1)
	assert.Equal(t, "iam.v1.organization.created", f.outbox.envelopes[0].Type)
}

func TestOnboardOrganizationUsesTheGivenSlug(t *testing.T) {
	f := newFixture(t)
	in := input()
	in.OrganizationSlug = new("engines")

	require.NoError(t, f.svc.OnboardOrganization(context.Background(), in))

	assert.Equal(t, "engines", f.orgs.writes[0].obj.(*iam.Organization).Slug())
}

func TestOnboardOrganizationRefusesConflictsBeforeWriting(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *fixture, in *iam.OnboardOrganizationInput)
		account bool
	}{
		{"email taken, in any case", func(f *fixture, in *iam.OnboardOrganizationInput) {
			f.accounts.emails["ada@example.com"] = true
			in.OwnerEmail = "ADA@example.com"
		}, true},
		{"name taken", func(f *fixture, _ *iam.OnboardOrganizationInput) {
			f.orgs.names["analytical engines ltd"] = true
		}, false},
		{"given slug taken", func(f *fixture, in *iam.OnboardOrganizationInput) {
			f.orgs.slugs["engines"] = true
			in.OrganizationSlug = new("engines")
		}, false},
		{"derived slug taken", func(f *fixture, _ *iam.OnboardOrganizationInput) {
			f.orgs.slugs["analytical-engines"] = true
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			in := input()
			tt.setup(f, in)

			err := f.svc.OnboardOrganization(context.Background(), in)

			if tt.account {
				_, ok := errors.AsType[*ErrAccountConflict](err)
				assert.True(t, ok, "account conflict, got %v", err)
			} else {
				_, ok := errors.AsType[*ErrOrganizationConflict](err)
				assert.True(t, ok, "organization conflict, got %v", err)
			}
			assert.Zero(t, f.tx.calls)
			assert.Empty(t, f.outbox.envelopes)
		})
	}
}

func TestOnboardOrganizationRefusesInvalidInput(t *testing.T) {
	tests := map[string]func(in *iam.OnboardOrganizationInput){
		"no organization display name": func(in *iam.OnboardOrganizationInput) { in.OrganizationDisplayName = "" },
		"bad slug":                     func(in *iam.OnboardOrganizationInput) { in.OrganizationSlug = new("Not A Slug") },
		"logo not over HTTPS":          func(in *iam.OnboardOrganizationInput) { in.OrganizationLogoURL = new("http://example.com/logo.png") },
		"bad email":                    func(in *iam.OnboardOrganizationInput) { in.OwnerEmail = "not-an-email" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			in := input()
			change(in)

			err := f.svc.OnboardOrganization(context.Background(), in)

			assert.ErrorIs(t, err, iam.ErrInvalid)
			assert.Zero(t, f.tx.calls)
		})
	}
}

// Another onboarding can take the slug or email between the check and the
// write; the unique index's error becomes the same conflict.
func TestOnboardOrganizationTurnsALostRaceIntoAConflict(t *testing.T) {
	t.Run("slug", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.err = fmt.Errorf("%w: duplicate key", iam.ErrAlreadyExists)

		_, ok := errors.AsType[*ErrOrganizationConflict](f.svc.OnboardOrganization(context.Background(), input()))

		assert.True(t, ok)
	})
	t.Run("email", func(t *testing.T) {
		f := newFixture(t)
		f.accounts.err = fmt.Errorf("%w: duplicate key", iam.ErrAlreadyExists)

		_, ok := errors.AsType[*ErrAccountConflict](f.svc.OnboardOrganization(context.Background(), input()))

		assert.True(t, ok)
	})
}

func TestOnboardOrganizationReturnsAndLogsFailedWrites(t *testing.T) {
	tests := map[string]func(f *fixture){
		"user":   func(f *fixture) { f.users.err = errBoom },
		"events": func(f *fixture) { f.outbox.err = errBoom },
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			fail(f)
			f.logger.On("Error", "failed to onboard organization", mock.Anything).Once()

			err := f.svc.OnboardOrganization(context.Background(), input())

			assert.ErrorIs(t, err, errBoom)
			f.logger.AssertExpectations(t)
		})
	}
}

func platformSignUp() iam.OnboardWithPlatformIdentityInput {
	return iam.OnboardWithPlatformIdentityInput{
		Provider:         iam.PlatformGitHub,
		Profile:          iam.ExternalProfile{Subject: "583231", Email: "grace@example.com", EmailVerified: true, DisplayName: "Grace Hopper"},
		OrganizationName: "Compilers Inc",
	}
}

func TestOnboardWithPlatformIdentity(t *testing.T) {
	f := newFixture(t)

	accountID, err := f.svc.OnboardWithPlatformIdentity(context.Background(), platformSignUp())

	require.NoError(t, err)
	account := f.accounts.writes[0].obj.(*iam.Account)
	assert.Equal(t, accountID, account.ID())
	assert.True(t, account.EmailVerified(), "the provider vouched for the email")
	assert.Equal(t, "Grace Hopper", account.DisplayName())
	link := f.platform.writes[0].obj.(*iam.AccountIdentity)
	assert.Equal(t, "583231", link.Subject())
	assert.Equal(t, accountID, link.AccountID())
	org := f.orgs.writes[0].obj.(*iam.Organization)
	assert.Equal(t, "compilers-inc", org.Slug())
	assert.Equal(t, "Compilers Inc", org.DisplayName(), "the display name defaults to the name")
	assert.Empty(t, f.passwords.writes, "no password")
	require.Len(t, f.grants.writes, 1)
	for name, w := range map[string][]write{"org": f.orgs.writes, "account": f.accounts.writes, "link": f.platform.writes, "user": f.users.writes, "grant": f.grants.writes} {
		assert.True(t, w[0].inTx, name)
	}
	assert.Equal(t, 1, f.tx.calls)
}

func TestOnboardWithPlatformIdentityRefuses(t *testing.T) {
	t.Run("an unverified email", func(t *testing.T) {
		f := newFixture(t)
		in := platformSignUp()
		in.Profile.EmailVerified = false
		_, err := f.svc.OnboardWithPlatformIdentity(context.Background(), in)
		assert.ErrorIs(t, err, iam.ErrInvalid)
		assert.Zero(t, f.tx.calls)
	})
	t.Run("an email that has an account", func(t *testing.T) {
		f := newFixture(t)
		f.accounts.emails["grace@example.com"] = true
		_, err := f.svc.OnboardWithPlatformIdentity(context.Background(), platformSignUp())
		_, ok := errors.AsType[*ErrAccountConflict](err)
		assert.True(t, ok)
	})
	t.Run("a taken slug", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.slugs["compilers-inc"] = true
		_, err := f.svc.OnboardWithPlatformIdentity(context.Background(), platformSignUp())
		_, ok := errors.AsType[*ErrOrganizationConflict](err)
		assert.True(t, ok)
	})
	t.Run("a subject linked in a race", func(t *testing.T) {
		f := newFixture(t)
		f.platform.err = fmt.Errorf("%w: duplicate", iam.ErrAlreadyExists)
		_, err := f.svc.OnboardWithPlatformIdentity(context.Background(), platformSignUp())
		_, ok := errors.AsType[*ErrAccountConflict](err)
		assert.True(t, ok)
	})
}

func TestOnboardOrganizationFailsWhenProvisioningFails(t *testing.T) {
	f := newFixture(t)
	f.provision.err = errors.New("workspace capability down")
	f.logger.On("Error", "failed to onboard organization", mock.Anything).Once()

	err := f.svc.OnboardOrganization(context.Background(), input())

	require.ErrorIs(t, err, f.provision.err)
}
