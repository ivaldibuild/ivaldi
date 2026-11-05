package signin

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher/hashertest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const adminEmail = "admin@acme.com"

type nopOutbox struct{}

func (nopOutbox) Record(context.Context, ...events.Envelope) error { return nil }

type fixture struct {
	ctx       context.Context
	db        *gorm.DB
	hasher    *hashertest.Fake
	org       *iam.Organization
	managed   *iam.IdentityProvider // configured, JIT on
	strict    *iam.IdentityProvider // configured, JIT off
	unmanaged *iam.IdentityProvider // created through the API, JIT on
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{ctx: context.Background(), db: databasetest.New(t, migrations.FS), hasher: hashertest.NewFake()}
	f.org = must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, nil))
	require.NoError(t, iampg.NewOrganizationRepository(f.db).Write(f.ctx, f.org))
	require.NoError(t, iampg.NewResourceRegistrar(f.db).RegisterResource(f.ctx, f.org.Resource()))
	f.managed = f.provider(t, iam.ProtocolGitHub, true, true)
	f.strict = f.provider(t, iam.ProtocolGoogle, true, false)
	f.unmanaged = f.provider(t, iam.ProtocolOIDC, false, true)
	return f
}

func (f *fixture) provider(t *testing.T, protocol iam.IdentityProtocol, managed, jit bool) *iam.IdentityProvider {
	t.Helper()
	config := map[iam.IdentityProtocol]iam.IdentityProviderConfig{
		iam.ProtocolGitHub: {ClientID: "gh", AllowedOrganizations: []string{"acme"}},
		iam.ProtocolGoogle: {ClientID: "g", AllowedDomains: []string{"acme.com"}},
		iam.ProtocolOIDC:   {ClientID: "o", Issuer: "https://sso.acme.com"},
	}[protocol]
	p := must(iam.NewIdentityProvider(iam.NewIdentityProviderProps{
		OrganizationID: f.org.ID(), Protocol: protocol, Name: string(protocol), Config: config,
		ManagedByConfiguration: managed, JITProvisioning: jit,
	}, nil))
	require.NoError(t, iampg.NewIdentityProviderRepository(f.db).Write(f.ctx, p))
	return p
}

func (f *fixture) service(t *testing.T, builtin bool) iam.Authentication {
	t.Helper()
	logger := logging.NewMockLogger()
	logger.On("Info", mock.Anything, mock.Anything, mock.Anything).Maybe()
	svc, err := New(
		WithLogger(logger),
		WithOrganizationRepo(iampg.NewOrganizationRepository(f.db)),
		WithAccountRepo(iampg.NewAccountRepository(f.db)),
		WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(f.db)),
		WithUserRepo(iampg.NewUserRepository(f.db)),
		WithIdentityProviderRepo(iampg.NewIdentityProviderRepository(f.db)),
		WithExternalIdentityRepo(iampg.NewExternalIdentityRepository(f.db)),
		WithAccountIdentityRepo(iampg.NewAccountIdentityRepository(f.db)),
		WithGrantRepo(iampg.NewGrantRepository(f.db)),
		WithPasswordHasher(f.hasher),
		WithTransactor(database.NewTransactor(f.db)),
		WithOutboxRecorder(nopOutbox{}),
		WithBuiltinAccounts(builtin),
		WithBootstrapAdminEmail(adminEmail),
	)
	require.NoError(t, err)
	return svc
}

// member creates an Account, optionally with a password, and its User in the Organization.
func (f *fixture) member(t *testing.T, email, password string) (*iam.Account, *iam.User) {
	t.Helper()
	a := must(iam.NewAccount(iam.NewAccountProps{Email: email, DisplayName: email}, nil))
	require.NoError(t, iampg.NewAccountRepository(f.db).Write(f.ctx, a))
	if password != "" {
		hash, _ := f.hasher.Hash(password)
		require.NoError(t, iampg.NewPasswordCredentialRepository(f.db).Write(f.ctx, must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: a.ID(), PasswordHash: hash}))))
	}
	u := must(iam.NewUser(iam.NewUserProps{PrincipalProps: iam.PrincipalProps{OrganizationID: f.org.ID(), DisplayName: email}, AccountID: a.ID()}, nil))
	require.NoError(t, iampg.NewUserRepository(f.db).Write(f.ctx, u))
	return a, u
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

func (f *fixture) owners(t *testing.T) int {
	t.Helper()
	role, _ := iam.BuiltInRoleID(iam.RoleOwner)
	return f.count(t, "SELECT count(*) FROM iam.grants WHERE role_id = ?", role)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func gitHubProfile(subject, email string) iam.ExternalProfile {
	return iam.ExternalProfile{Subject: subject, Email: email, EmailVerified: true, DisplayName: "Ada", Organizations: []string{"acme"}}
}

func TestAuthenticatePassword(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, true)
	ada, adaUser := f.member(t, "ada@acme.com", "correct-horse")
	f.member(t, "nopw@acme.com", "")

	t.Run("right password", func(t *testing.T) {
		got, err := svc.AuthenticatePassword(f.ctx, "ADA@acme.com", "correct-horse")
		require.NoError(t, err)
		assert.Equal(t, ada.ID(), got.AccountID)
		assert.Equal(t, iam.SignInPassword, got.Method)
		assert.Empty(t, got.RestrictedTo)
		require.Len(t, got.Memberships, 1)
		assert.Equal(t, adaUser.ID(), got.Memberships[0].PrincipalID)
	})

	t.Run("wrong email and wrong password look the same", func(t *testing.T) {
		verifies, dummies := f.hasher.Checks()
		_, unknown := svc.AuthenticatePassword(f.ctx, "nobody@acme.com", "correct-horse")
		_, noPassword := svc.AuthenticatePassword(f.ctx, "nopw@acme.com", "x")
		_, wrong := svc.AuthenticatePassword(f.ctx, "ada@acme.com", "Correct-horse")

		assert.ErrorIs(t, unknown, iam.ErrInvalidCredentials)
		assert.ErrorIs(t, noPassword, iam.ErrInvalidCredentials)
		assert.ErrorIs(t, wrong, iam.ErrInvalidCredentials)
		v, d := f.hasher.Checks()
		assert.Equal(t, 1, v-verifies, "the wrong password was checked")
		assert.Equal(t, 2, d-dummies, "a check was spent for each account without a password, so timing reveals nothing")
	})

	t.Run("suspended account", func(t *testing.T) {
		grace, _ := f.member(t, "grace@acme.com", "pw")
		require.NoError(t, grace.Suspend())
		require.NoError(t, iampg.NewAccountRepository(f.db).Write(f.ctx, grace))

		_, err := svc.AuthenticatePassword(f.ctx, "grace@acme.com", "pw")
		assert.ErrorIs(t, err, iam.ErrStatus)
	})

	t.Run("suspended memberships are not offered", func(t *testing.T) {
		_, u := f.member(t, "linus@acme.com", "pw")
		require.NoError(t, u.Suspend())
		require.NoError(t, iampg.NewUserRepository(f.db).Write(f.ctx, u))

		got, err := svc.AuthenticatePassword(f.ctx, "linus@acme.com", "pw")
		require.NoError(t, err)
		assert.Empty(t, got.Memberships)
	})

	t.Run("built-in accounts off", func(t *testing.T) {
		_, err := f.service(t, false).AuthenticatePassword(f.ctx, "ada@acme.com", "correct-horse")
		assert.ErrorIs(t, err, iam.ErrNotAdmitted)
	})
}

func TestAuthenticateExternalProvisions(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, false)

	got, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), f.managed.ID(), gitHubProfile("1001", "new@acme.com"))

	require.NoError(t, err)
	assert.Equal(t, iam.SignInIdentityProvider, got.Method)
	assert.Equal(t, f.org.ID(), got.RestrictedTo, "an Organization's provider signs in to that Organization only")
	require.Len(t, got.Memberships, 1)
	assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.accounts WHERE email = 'new@acme.com' AND email_verified_at IS NOT NULL"))
	assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.external_identities WHERE subject = '1001'"))

	again, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), f.managed.ID(), gitHubProfile("1001", "renamed@acme.com"))
	require.NoError(t, err)
	assert.Equal(t, got.AccountID, again.AccountID, "the subject, not the email, identifies the person next time")
	assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.accounts"))
}

func TestAuthenticateExternalLinksAnExistingAccount(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, false)
	ada, adaUser := f.member(t, "ada@acme.com", "pw")

	got, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), f.managed.ID(), gitHubProfile("1001", "Ada@acme.com"))

	require.NoError(t, err)
	assert.Equal(t, ada.ID(), got.AccountID)
	assert.Equal(t, adaUser.ID(), got.Memberships[0].PrincipalID)
	assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.accounts"))
	assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.users"))
}

func TestAuthenticateExternalRefuses(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, false)
	f.member(t, "taken@acme.com", "pw")
	_, suspended := f.member(t, "suspended@acme.com", "")
	require.NoError(t, suspended.Suspend())
	require.NoError(t, iampg.NewUserRepository(f.db).Write(f.ctx, suspended))

	tests := []struct {
		name    string
		idp     func() *iam.IdentityProvider
		profile iam.ExternalProfile
		want    error
	}{
		{"not in an allowed organization", func() *iam.IdentityProvider { return f.managed },
			iam.ExternalProfile{Subject: "2", Email: "x@acme.com", EmailVerified: true, Organizations: []string{"other"}}, iam.ErrNotAdmitted},
		{"no verified email on a first sign-in", func() *iam.IdentityProvider { return f.managed },
			iam.ExternalProfile{Subject: "3", Email: "x@acme.com", Organizations: []string{"acme"}}, iam.ErrNotAdmitted},
		{"no provisioning", func() *iam.IdentityProvider { return f.strict },
			iam.ExternalProfile{Subject: "4", Email: "x@acme.com", EmailVerified: true, HostedDomain: "acme.com"}, iam.ErrNoMembership},
		{"a provider the configuration does not manage cannot take over an account", func() *iam.IdentityProvider { return f.unmanaged },
			iam.ExternalProfile{Subject: "5", Email: "taken@acme.com", EmailVerified: true}, iam.ErrAccountExists},
		{"suspended membership", func() *iam.IdentityProvider { return f.managed }, gitHubProfile("6", "suspended@acme.com"), iam.ErrStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts, links := f.count(t, "SELECT count(*) FROM iam.accounts"), f.count(t, "SELECT count(*) FROM iam.external_identities")

			_, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), tt.idp().ID(), tt.profile)

			assert.ErrorIs(t, err, tt.want)
			assert.Equal(t, accounts, f.count(t, "SELECT count(*) FROM iam.accounts"), "nothing is provisioned")
			assert.Equal(t, links, f.count(t, "SELECT count(*) FROM iam.external_identities"))
		})
	}

	t.Run("disabled provider", func(t *testing.T) {
		require.NoError(t, f.strict.Unconfigure())
		require.NoError(t, iampg.NewIdentityProviderRepository(f.db).Write(f.ctx, f.strict))
		_, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), f.strict.ID(), iam.ExternalProfile{Subject: "7", HostedDomain: "acme.com"})
		assert.ErrorIs(t, err, iam.ErrNotAdmitted)
	})
}

func TestBootstrapAdministratorClaimsTheOrganization(t *testing.T) {
	t.Run("once, even through a provider that does not provision", func(t *testing.T) {
		f := newFixture(t)
		svc := f.service(t, false)
		admin := iam.ExternalProfile{Subject: "g-1", Email: "Admin@acme.com", EmailVerified: true, HostedDomain: "acme.com"}

		_, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), f.strict.ID(), admin)
		require.NoError(t, err)
		assert.Equal(t, 1, f.owners(t))

		_, err = svc.AuthenticateExternal(f.ctx, f.org.ID(), f.strict.ID(), admin)
		require.NoError(t, err)
		assert.Equal(t, 1, f.owners(t), "a second sign-in grants nothing")
	})

	t.Run("not through a provider the configuration does not manage", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.service(t, false).AuthenticateExternal(f.ctx, f.org.ID(), f.unmanaged.ID(), iam.ExternalProfile{Subject: "o-1", Email: adminEmail, EmailVerified: true})
		require.NoError(t, err)
		assert.Zero(t, f.owners(t))
	})

	t.Run("not once the Organization has an owner", func(t *testing.T) {
		f := newFixture(t)
		_, founder := f.member(t, "founder@acme.com", "")
		require.NoError(t, iampg.NewGrantRepository(f.db).Write(f.ctx, must(iam.NewOwnerGrant(f.org.ID(), founder.ID()))))

		_, err := f.service(t, false).AuthenticateExternal(f.ctx, f.org.ID(), f.managed.ID(), gitHubProfile("1", adminEmail))
		require.NoError(t, err)
		assert.Equal(t, 1, f.owners(t))
	})

	t.Run("one grant when the administrator signs in twice at once", func(t *testing.T) {
		f := newFixture(t)
		svc := f.service(t, false)
		f.member(t, adminEmail, "")
		profiles := map[*iam.IdentityProvider]iam.ExternalProfile{
			f.managed: gitHubProfile("gh-admin", adminEmail),
			f.strict:  {Subject: "g-admin", Email: adminEmail, EmailVerified: true, HostedDomain: "acme.com"},
		}

		var wg sync.WaitGroup
		for idp, p := range profiles {
			wg.Go(func() {
				_, err := svc.AuthenticateExternal(f.ctx, f.org.ID(), idp.ID(), p)
				assert.NoError(t, err)
			})
		}
		wg.Wait()

		assert.Equal(t, 1, f.owners(t))
	})
}

func TestMembership(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, true)
	ada, adaUser := f.member(t, "ada@acme.com", "pw")

	m, err := svc.Membership(f.ctx, ada.ID(), f.org.ID())
	require.NoError(t, err)
	assert.Equal(t, adaUser.ID(), m.PrincipalID)

	all, err := svc.Memberships(f.ctx, ada.ID())
	require.NoError(t, err)
	assert.Equal(t, []string{adaUser.ID()}, []string{all[0].PrincipalID})

	_, err = svc.Membership(f.ctx, ada.ID(), "org_other")
	assert.ErrorIs(t, err, iam.ErrNoMembership)

	require.NoError(t, ada.Deactivate())
	require.NoError(t, iampg.NewAccountRepository(f.db).Write(f.ctx, ada))
	_, err = svc.Membership(f.ctx, ada.ID(), f.org.ID())
	assert.ErrorIs(t, err, iam.ErrNoMembership, "a deactivated account loses every membership")
	all, err = svc.Memberships(f.ctx, ada.ID())
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestNewRequiresEveryDependency(t *testing.T) {
	_, err := New()
	assert.ErrorContains(t, err, "is required")
}

func TestAuthenticatePlatform(t *testing.T) {
	f := newFixture(t)
	svc := f.service(t, false)
	profile := func(subject, email string, verified bool) iam.ExternalProfile {
		return iam.ExternalProfile{Subject: subject, Email: email, EmailVerified: verified}
	}

	t.Run("an unknown person must sign up", func(t *testing.T) {
		_, err := svc.AuthenticatePlatform(f.ctx, iam.PlatformGitHub, profile("1", "new@example.com", true))
		assert.ErrorIs(t, err, iam.ErrSignUpRequired)
	})

	t.Run("an Account with a verified email is linked", func(t *testing.T) {
		ada, adaUser := f.member(t, "ada@example.com", "pw")
		ada.VerifyEmail()
		require.NoError(t, iampg.NewAccountRepository(f.db).Write(f.ctx, ada))

		got, err := svc.AuthenticatePlatform(f.ctx, iam.PlatformGitHub, profile("2", "ADA@example.com", true))
		require.NoError(t, err)
		assert.Equal(t, ada.ID(), got.AccountID)
		assert.Equal(t, iam.SignInPlatform, got.Method)
		assert.Empty(t, got.RestrictedTo, "a platform sign-in may choose any of its Organizations")
		require.Len(t, got.Memberships, 1)
		assert.Equal(t, adaUser.ID(), got.Memberships[0].PrincipalID)
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.account_identities WHERE subject = '2'"))

		again, err := svc.AuthenticatePlatform(f.ctx, iam.PlatformGitHub, profile("2", "changed@example.com", false))
		require.NoError(t, err)
		assert.Equal(t, ada.ID(), again.AccountID, "the subject is enough once linked")
	})

	t.Run("an Account whose email was never verified is not taken over", func(t *testing.T) {
		f.member(t, "squatted@example.com", "pw")
		_, err := svc.AuthenticatePlatform(f.ctx, iam.PlatformGoogle, profile("3", "squatted@example.com", true))
		assert.ErrorIs(t, err, iam.ErrAccountExists)
		assert.Zero(t, f.count(t, "SELECT count(*) FROM iam.account_identities WHERE subject = '3'"))
	})

	t.Run("no verified email from the provider", func(t *testing.T) {
		_, err := svc.AuthenticatePlatform(f.ctx, iam.PlatformGoogle, profile("4", "ada@example.com", false))
		assert.ErrorIs(t, err, iam.ErrNotAdmitted)
	})
}
