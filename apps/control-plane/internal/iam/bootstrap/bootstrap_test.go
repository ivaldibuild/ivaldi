package bootstrap

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
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// recordingOutbox keeps the events bootstrap records.
type recordingOutbox struct {
	mu        sync.Mutex
	envelopes []events.Envelope
}

func (o *recordingOutbox) Record(_ context.Context, envelopes ...events.Envelope) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.envelopes = append(o.envelopes, envelopes...)
	return nil
}

func (o *recordingOutbox) types() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.envelopes))
	for i, e := range o.envelopes {
		out[i] = e.Type
	}
	return out
}

func (o *recordingOutbox) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.envelopes = nil
}

type env struct {
	db     *gorm.DB
	outbox *recordingOutbox
	hasher *hashertest.Fake
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{db: databasetest.New(t, migrations.FS), outbox: &recordingOutbox{}, hasher: hashertest.NewFake()}
}

func (e *env) bootstrapper(t *testing.T, s Settings) Bootstrapper {
	t.Helper()
	logger := logging.NewMockLogger()
	logger.On("Info", mock.Anything, mock.Anything).Maybe()
	logger.On("Info", mock.Anything, mock.Anything, mock.Anything).Maybe()
	logger.On("Warn", mock.Anything, mock.Anything).Maybe()
	tx := database.NewTransactor(e.db)
	resources := iampg.NewResourceRegistrar(e.db)
	grants := iampg.NewGrantRepository(e.db)
	ob, err := onboarding.NewOnboardingService(
		onboarding.WithLogger(logger),
		onboarding.WithOrganizationRepo(iampg.NewOrganizationRepository(e.db)),
		onboarding.WithAccountRepo(iampg.NewAccountRepository(e.db)),
		onboarding.WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(e.db)),
		onboarding.WithUserRepo(iampg.NewUserRepository(e.db)),
		onboarding.WithTransactor(tx),
		onboarding.WithOutboxRecorder(e.outbox),
		onboarding.WithPasswordHasher(e.hasher),
		onboarding.WithResourceRegistrar(resources),
		onboarding.WithGrantRepo(grants),
		onboarding.WithAccountIdentityRepo(iampg.NewAccountIdentityRepository(e.db)),
		onboarding.WithOrganizationProvisioner(&provisioned{}),
	)
	require.NoError(t, err)
	b, err := New(
		WithSettings(s),
		WithLogger(logger),
		WithOnboarding(ob),
		WithOrganizationRepo(iampg.NewOrganizationRepository(e.db)),
		WithIdentityProviderRepo(iampg.NewIdentityProviderRepository(e.db)),
		WithAccountRepo(iampg.NewAccountRepository(e.db)),
		WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(e.db)),
		WithUserRepo(iampg.NewUserRepository(e.db)),
		WithGrantRepo(grants),
		WithOAuthClientRepo(iampg.NewOAuthClientRepository(e.db)),
		WithWorkloadRepo(iampg.NewWorkloadRepository(e.db)),
		WithWorkloadCredentialRepo(iampg.NewWorkloadCredentialRepository(e.db)),
		WithPasswordHasher(e.hasher),
		WithTransactor(tx),
		WithLocker(database.NewLocker(e.db)),
		WithOutboxRecorder(e.outbox),
	)
	require.NoError(t, err)
	return b
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, e.db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

var (
	cliClient = ClientSettings{ClientID: "ivaldictl", Name: "Ivaldi CLI", RedirectURIs: []string{"http://127.0.0.1/callback", "http://[::1]/callback"}}
	github    = ProviderSettings{Protocol: iam.ProtocolGitHub, Name: "GitHub", Config: iam.IdentityProviderConfig{ClientID: "Iv1.abc", AllowedOrganizations: []string{"acme"}}, JITProvisioning: true}
	builtin   = ProviderSettings{Protocol: iam.ProtocolBuiltin, Name: "Email and password"}
)

func selfHosted() Settings {
	return Settings{
		Mode:         ModeSelfHosted,
		Clients:      []ClientSettings{cliClient},
		Organization: OrganizationSettings{Slug: "acme", Name: "Acme"},
		AdminEmail:   "ada@acme.com",
		Providers:    []ProviderSettings{github, builtin},
	}
}

func TestBootstrapAnEmptyInstallation(t *testing.T) {
	e := newEnv(t)

	inst, err := e.bootstrapper(t, selfHosted()).Bootstrap(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, inst.OrganizationID)
	assert.Len(t, inst.Providers, 2)
	assert.Contains(t, inst.Providers, iam.ProtocolGitHub)
	assert.Contains(t, inst.Providers, iam.ProtocolBuiltin)
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.organizations WHERE slug = 'acme'"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.resources WHERE id = ?", inst.OrganizationID))
	assert.Equal(t, 2, e.count(t, "SELECT count(*) FROM iam.identity_providers WHERE config->>'managed_by' = 'configuration' AND status = 'active'"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.oauth_clients WHERE client_id = 'ivaldictl' AND registration = 'first_party'"))
	assert.Zero(t, e.count(t, "SELECT count(*) FROM iam.grants"), "the administrator claims ownership at first sign-in")
	assert.Zero(t, e.count(t, "SELECT count(*) FROM iam.accounts"))
	assert.ElementsMatch(t, []string{
		"iam.v1.oauth_client.created", "iam.v1.organization.created",
		"iam.v1.identity_provider.created", "iam.v1.identity_provider.created",
	}, e.outbox.types())
}

func TestBootstrapAgainChangesNothing(t *testing.T) {
	e := newEnv(t)
	first, err := e.bootstrapper(t, selfHosted()).Bootstrap(context.Background())
	require.NoError(t, err)
	e.outbox.reset()

	again, err := e.bootstrapper(t, selfHosted()).Bootstrap(context.Background())

	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Empty(t, e.outbox.types(), "nothing changed, so nothing is recorded")
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.organizations"))
	assert.Equal(t, 2, e.count(t, "SELECT count(*) FROM iam.identity_providers"))
}

func TestBootstrapAppliesConfigurationChanges(t *testing.T) {
	e := newEnv(t)
	_, err := e.bootstrapper(t, selfHosted()).Bootstrap(context.Background())
	require.NoError(t, err)
	e.outbox.reset()

	changed := selfHosted()
	moreOrgs := github
	moreOrgs.Config.AllowedOrganizations = []string{"acme", "acme-labs"}
	changed.Providers = []ProviderSettings{moreOrgs} // built-in accounts turned off
	changed.Clients[0].RedirectURIs = []string{"http://127.0.0.1/callback"}

	inst, err := e.bootstrapper(t, changed).Bootstrap(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []iam.IdentityProtocol{iam.ProtocolGitHub}, keys(inst.Providers))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.identity_providers WHERE protocol = 'builtin' AND status = 'disabled'"), "a provider dropped from the configuration is disabled, not deleted")
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.identity_providers WHERE protocol = 'github' AND config->'allowed_organizations' ? 'acme-labs'"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.oauth_clients WHERE client_id = 'ivaldictl' AND cardinality(redirect_uris) = 1"))
	assert.ElementsMatch(t, []string{
		"iam.v1.oauth_client.updated", "iam.v1.identity_provider.configured", "iam.v1.identity_provider.status_changed",
	}, e.outbox.types())
}

func TestBootstrapSeedsTheAdministratorOnce(t *testing.T) {
	e := newEnv(t)
	s := selfHosted()
	s.AdminPassword = "correct-horse"

	inst, err := e.bootstrapper(t, s).Bootstrap(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.accounts WHERE email = 'ada@acme.com' AND email_verified_at IS NOT NULL"))
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.grants WHERE role_id = ? AND resource_id = ?", ownerRole, inst.OrganizationID))
	var hash string
	require.NoError(t, e.db.Raw("SELECT password_hash FROM iam.password_credentials").Scan(&hash).Error)
	ok, err := e.hasher.Verify("correct-horse", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	s.AdminPassword = "a-new-password"
	_, err = e.bootstrapper(t, s).Bootstrap(context.Background())
	require.NoError(t, err)
	require.NoError(t, e.db.Raw("SELECT password_hash FROM iam.password_credentials").Scan(&hash).Error)
	ok, err = e.hasher.Verify("correct-horse", hash)
	require.NoError(t, err)
	assert.True(t, ok, "the password file never replaces a password")
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.grants"))
}

func TestBootstrapConcurrentReplicasCreateOneOrganization(t *testing.T) {
	e := newEnv(t)

	var wg sync.WaitGroup
	results := make([]*Installation, 4)
	errs := make([]error, 4)
	for i := range results {
		b := e.bootstrapper(t, selfHosted())
		wg.Go(func() { results[i], errs[i] = b.Bootstrap(context.Background()) })
	}
	wg.Wait()

	for i := range results {
		require.NoError(t, errs[i])
		assert.Equal(t, results[0].OrganizationID, results[i].OrganizationID)
	}
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.organizations"))
	assert.Equal(t, 2, e.count(t, "SELECT count(*) FROM iam.identity_providers"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.oauth_clients"))
}

func TestBootstrapInTheCloudOnlyRegistersClients(t *testing.T) {
	e := newEnv(t)

	inst, err := e.bootstrapper(t, Settings{Mode: ModeCloud, Clients: []ClientSettings{cliClient}}).Bootstrap(context.Background())

	require.NoError(t, err)
	assert.Empty(t, inst.OrganizationID)
	assert.Zero(t, e.count(t, "SELECT count(*) FROM iam.organizations"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM iam.oauth_clients"))
	require.Len(t, e.outbox.envelopes, 1)
	assert.Empty(t, e.outbox.envelopes[0].OrganizationID, "a client belongs to no Organization")
}

func TestBootstrapRefuses(t *testing.T) {
	t.Run("an empty installation without bootstrap settings", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.bootstrapper(t, Settings{Mode: ModeSelfHosted}).Bootstrap(context.Background())
		assert.ErrorIs(t, err, ErrNotConfigured)
	})
	t.Run("a self-hosted installation with two organizations", func(t *testing.T) {
		e := newEnv(t)
		for _, slug := range []string{"one", "two"} {
			org, err := iam.NewOrganization(iam.NewOrganizationProps{Slug: slug, DisplayName: slug}, nil)
			require.NoError(t, err)
			require.NoError(t, iampg.NewOrganizationRepository(e.db).Write(context.Background(), org))
		}
		_, err := e.bootstrapper(t, selfHosted()).Bootstrap(context.Background())
		assert.ErrorContains(t, err, "found 2")
	})
}

func TestNewRequiresEveryDependency(t *testing.T) {
	_, err := New(WithSettings(Settings{Mode: ModeSelfHosted}))
	assert.ErrorContains(t, err, "is required")
}

func keys(m map[iam.IdentityProtocol]string) []iam.IdentityProtocol {
	out := make([]iam.IdentityProtocol, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// provisioned records the Organizations onboarding provisions, standing in
// for the workspace capability.
type provisioned struct{ orgs []string }

func (p *provisioned) ProvisionOrganization(_ context.Context, organizationID string) error {
	p.orgs = append(p.orgs, organizationID)
	return nil
}
