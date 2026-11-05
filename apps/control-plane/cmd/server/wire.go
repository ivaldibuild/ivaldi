package server

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/bootstrap"
	iamhttp "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/httpapi"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// app is what serve runs, built from the configuration. It is the one place
// concrete implementations are chosen; everything else depends on
// interfaces.
type app struct {
	db         *gorm.DB
	tx         transaction.Transactor
	outbox     outbox.Store
	iam        iamRepositories
	onboarding iam.Onboarding
	install    *bootstrap.Installation
	keys       token.KeyRing
	verifier   identity.Verifier
	routes     []api.Routes
	handlers   []api.Handlers
}

// tokenLeeway allows for clock skew between replicas verifying tokens.
const tokenLeeway = 30 * time.Second

// iamRepositories are IAM's PostgreSQL repositories, shared by its services.
type iamRepositories struct {
	orgs      iam.OrganizationRepository
	accounts  iam.AccountRepository
	passwords iam.PasswordCredentialRepository
	users     iam.UserRepository
	grants    iam.GrantRepository
	resources iam.ResourceRegistrar
	providers iam.IdentityProviderRepository
	clients   iam.OAuthClientRepository
}

// wire opens the database and builds the capabilities. The caller closes the
// database with close once the server has stopped.
func wire(ctx context.Context, cfg *config.Config, logger logging.Logger) (a *app, close func() error, err error) {
	db, close, err := openDatabase(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = close()
		}
	}()
	logger.Info("database connected")

	a = &app{
		db:     db,
		tx:     database.NewTransactor(db),
		outbox: outboxpg.NewRepository(db),
		iam: iamRepositories{
			orgs:      iampg.NewOrganizationRepository(db),
			accounts:  iampg.NewAccountRepository(db),
			passwords: iampg.NewPasswordCredentialRepository(db),
			users:     iampg.NewUserRepository(db),
			grants:    iampg.NewGrantRepository(db),
			resources: iampg.NewResourceRegistrar(db),
			providers: iampg.NewIdentityProviderRepository(db),
			clients:   iampg.NewOAuthClientRepository(db),
		},
	}

	a.keys, err = loadKeys(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	a.verifier, err = identity.NewVerifier(identity.VerifierConfig{
		Issuer:   cfg.IssuerURL(),
		Audience: cfg.IssuerURL(),
		Keys:     identity.NewStaticKeySource(a.keys.PublicKeys()...),
		Leeway:   tokenLeeway,
	})
	if err != nil {
		return nil, nil, err
	}
	a.handlers = append(a.handlers, authhttp.NewWellKnown(a.keys))

	passwords, err := hasher.NewArgon2id()
	if err != nil {
		return nil, nil, fmt.Errorf("password hasher: %w", err)
	}
	a.onboarding, err = onboarding.NewOnboardingService(
		onboarding.WithLogger(logger),
		onboarding.WithOrganizationRepo(a.iam.orgs),
		onboarding.WithAccountRepo(a.iam.accounts),
		onboarding.WithPasswordCredentialRepo(a.iam.passwords),
		onboarding.WithUserRepo(a.iam.users),
		onboarding.WithTransactor(a.tx),
		onboarding.WithOutboxRecorder(a.outbox),
		onboarding.WithPasswordHasher(passwords),
		onboarding.WithResourceRegistrar(a.iam.resources),
		onboarding.WithGrantRepo(a.iam.grants),
	)
	if err != nil {
		return nil, nil, err
	}

	settings, err := bootstrapSettings(cfg)
	if err != nil {
		return nil, nil, err
	}
	boot, err := bootstrap.New(
		bootstrap.WithSettings(settings),
		bootstrap.WithLogger(logger),
		bootstrap.WithOnboarding(a.onboarding),
		bootstrap.WithOrganizationRepo(a.iam.orgs),
		bootstrap.WithIdentityProviderRepo(a.iam.providers),
		bootstrap.WithAccountRepo(a.iam.accounts),
		bootstrap.WithPasswordCredentialRepo(a.iam.passwords),
		bootstrap.WithUserRepo(a.iam.users),
		bootstrap.WithGrantRepo(a.iam.grants),
		bootstrap.WithOAuthClientRepo(a.iam.clients),
		bootstrap.WithPasswordHasher(passwords),
		bootstrap.WithTransactor(a.tx),
		bootstrap.WithLocker(database.NewLocker(db)),
		bootstrap.WithOutboxRecorder(a.outbox),
	)
	if err != nil {
		return nil, nil, err
	}
	if a.install, err = boot.Bootstrap(ctx); err != nil {
		return nil, nil, fmt.Errorf("bootstrap: %w", err)
	}

	// Anyone may call onboarding, so it is only offered where anyone may
	// create an Organization: the managed cloud, with built-in accounts. A
	// self-hosted installation's Organization comes from its configuration.
	if cfg.Mode() == config.DeploymentModeCloud && cfg.Auth.BuiltinAccounts {
		a.routes = append(a.routes, iamhttp.NewOnboardingController(a.onboarding))
	}
	return a, close, nil
}

// loadKeys reads the signing keys, or generates one when the configuration
// asks for an ephemeral key.
func loadKeys(cfg *config.Config, logger logging.Logger) (token.KeyRing, error) {
	if len(cfg.Auth.SigningKeyFiles) > 0 {
		return token.LoadKeyRing(cfg.Auth.SigningKeyFiles)
	}
	logger.Warn("signing tokens with an ephemeral key: they stop working on restart and other replicas cannot verify them; set auth.signing_key_files outside local development")
	return token.NewEphemeralKeyRing()
}

// openDatabase connects with the configured pool and checks the connection,
// so a wrong address fails at start rather than at the first request.
func openDatabase(ctx context.Context, cfg *config.Config) (*gorm.DB, func() error, error) {
	dsn, err := cfg.DatabaseURL()
	if err != nil {
		return nil, nil, err
	}
	maxPool, minPool, idleTimeout, connectTimeout, err := cfg.DatabasePoolOptions()
	if err != nil {
		return nil, nil, err
	}

	db, err := database.Open(dsn)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxPool)
	sqlDB.SetMaxIdleConns(minPool)
	sqlDB.SetConnMaxIdleTime(idleTimeout)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("connect to database: %w", err)
	}
	return db, sqlDB.Close, nil
}
