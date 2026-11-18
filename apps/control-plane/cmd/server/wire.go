package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	deliverypg "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/enrollment"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/apitokens"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/bootstrap"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/grants"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/oauthserver"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/organizations"
	peopleapi "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/people"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	principalsapi "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/principals"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/profile"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/serviceaccounts"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/signin"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/workloadauth"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration/agents"
	orchestrationpg "github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	teampg "github.com/ivaldibuild/ivaldi-control-plane/internal/team/postgres"
	versioncontrolpg "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/webhookroutes"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/webhooks"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/events/redisstream"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// app is what serve runs, built from the configuration. It is the one place
// concrete implementations are chosen; everything else depends on
// interfaces.
type app struct {
	db         *gorm.DB
	tx         transaction.Transactor
	outbox     outbox.Store
	redis      *redis.Client
	relay      *outbox.Relay
	events     events.Consumer
	iam        iamRepositories
	onboarding iam.Onboarding
	auth       iam.Authentication
	oauth      iam.AuthorizationServer
	install    *bootstrap.Installation
	authz      authorization.Authorizer
	audit      audit.Recorder
	// approvals opens and cancels approval requests for the capabilities
	// whose subjects wait, which register in approvalSubjects.
	approvals        policy.Approvals
	approvalSubjects *policy.Subjects
	teams            team.Directory
	// settingDefs, settingLocks, and settingResolver are shared by the
	// capabilities that define, lock, and read settings.
	settingDefs     *workspace.Definitions
	settingLocks    workspace.LockSource
	settingResolver workspace.SettingResolver
	// provisioner sets up each new Organization's default Workspace.
	provisioner iam.OrganizationProvisioner
	keys        token.KeyRing
	verifier    identity.Verifier
	// kubernetes verifies projected service-account tokens for workloads.
	kubernetes workloadauth.KubernetesVerifier
	// services serve the API's operations; root.go builds its routes from them.
	services services
	// version is the binary's, used to name the agent chart when no version is
	// configured.
	version string
	// commitHistory reads commit ranges for delivery's release notes.
	commitHistory vcs.History
	// changes reads recorded Changes for release notes.
	changes  provenance.Lookup
	handlers []api.Handlers
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
	externals iam.ExternalIdentityRepository
	platform  iam.AccountIdentityRepository
	codes     iam.OAuthAuthorizationCodeRepository
	refreshes iam.OAuthRefreshTokenRepository
}

// wire opens the database and builds the capabilities. The caller closes the
// database and the Redis connection with close once the server has stopped.
func wire(ctx context.Context, cfg *config.Config, version string, logger logging.Logger) (a *app, close func() error, err error) {
	db, closeDB, err := openDatabase(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	redisClient, err := openRedis(cfg.Redis)
	if err != nil {
		_ = closeDB()
		return nil, nil, err
	}
	close = func() error {
		return errors.Join(closeDB(), redisClient.Close())
	}
	defer func() {
		if err != nil {
			_ = close()
		}
	}()
	logger.Info("database connected")

	a = &app{
		db:      db,
		tx:      database.NewTransactor(db),
		outbox:  outboxpg.NewRepository(db),
		redis:   redisClient,
		version: version,
		iam: iamRepositories{
			orgs:      iampg.NewOrganizationRepository(db),
			accounts:  iampg.NewAccountRepository(db),
			passwords: iampg.NewPasswordCredentialRepository(db),
			users:     iampg.NewUserRepository(db),
			grants:    iampg.NewGrantRepository(db),
			resources: iampg.NewResourceRegistrar(db),
			providers: iampg.NewIdentityProviderRepository(db),
			clients:   iampg.NewOAuthClientRepository(db),
			externals: iampg.NewExternalIdentityRepository(db),
			platform:  iampg.NewAccountIdentityRepository(db),
			codes:     iampg.NewOAuthAuthorizationCodeRepository(db),
			refreshes: iampg.NewOAuthRefreshTokenRepository(db),
		},
	}

	if err := a.wireOutbox(cfg, logger); err != nil {
		return nil, nil, err
	}

	a.keys, err = loadKeys(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	accessTokens, err := identity.NewVerifier(identity.VerifierConfig{
		Issuer:   cfg.IssuerURL(),
		Audience: cfg.IssuerURL(),
		Keys:     identity.NewStaticKeySource(a.keys.PublicKeys()...),
		Leeway:   tokenLeeway,
	})
	if err != nil {
		return nil, nil, err
	}
	// API tokens, which service accounts such as CI use, are checked against
	// the database; every other bearer token is an access token, verified
	// offline.
	apiTokensVerifier, err := apitokens.NewVerifier(iampg.NewAPITokenRepository(db), iampg.NewPrincipalRepository(db), accessTokens)
	if err != nil {
		return nil, nil, err
	}
	a.kubernetes, err = workloadauth.NewKubernetesVerifier(
		cfg.Runners.KubernetesIssuers, cfg.Runners.KubernetesAudience, nil)
	if err != nil {
		return nil, nil, err
	}
	a.verifier, err = workloadauth.NewVerifier(
		iampg.NewWorkloadCredentialRepository(db), iampg.NewWorkloadRepository(db), a.kubernetes, apiTokensVerifier)
	if err != nil {
		return nil, nil, err
	}
	a.handlers = append(a.handlers, authhttp.NewWellKnown(a.keys))

	passwords, err := hasher.NewArgon2id()
	if err != nil {
		return nil, nil, fmt.Errorf("password hasher: %w", err)
	}
	// Every capability writes its audit records through one recorder and
	// authorizes through one authorizer, and onboarding provisions each new
	// Organization's default Workspace, so these come first.
	if err := a.wireAccess(logger); err != nil {
		return nil, nil, err
	}
	if err := a.wireProvisioner(); err != nil {
		return nil, nil, err
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
		onboarding.WithAccountIdentityRepo(a.iam.platform),
		onboarding.WithOrganizationProvisioner(a.provisioner),
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
		bootstrap.WithWorkloadRepo(iampg.NewWorkloadRepository(db)),
		bootstrap.WithWorkloadCredentialRepo(iampg.NewWorkloadCredentialRepository(db)),
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

	if err := a.wireSignIn(cfg, logger, passwords); err != nil {
		return nil, nil, err
	}

	if err := a.wireSettings(); err != nil {
		return nil, nil, err
	}
	if err := a.wireTeams(); err != nil {
		return nil, nil, err
	}
	if err := a.wireVersionControl(cfg); err != nil {
		return nil, nil, err
	}
	if err := a.wireWebhooks(cfg, logger); err != nil {
		return nil, nil, err
	}
	if err := a.wireProvenance(); err != nil {
		return nil, nil, err
	}
	if err := a.wireAgents(); err != nil {
		return nil, nil, err
	}
	if err := a.wireDelivery(cfg.Runners); err != nil {
		return nil, nil, err
	}
	if err := a.wireApprovals(); err != nil {
		return nil, nil, err
	}
	if err := a.wireWorkspace(); err != nil {
		return nil, nil, err
	}
	if err := a.wireProfile(); err != nil {
		return nil, nil, err
	}
	if err := a.wireOrganization(); err != nil {
		return nil, nil, err
	}
	if err := a.wireAccessAPI(); err != nil {
		return nil, nil, err
	}
	if err := a.wireServiceAccounts(); err != nil {
		return nil, nil, err
	}
	if err := a.wireEnrollment(cfg.Runners, cfg.ExternalURL, logger); err != nil {
		return nil, nil, err
	}

	// Anyone may call onboarding, so it is only offered where anyone may
	// create an Organization: the managed cloud, with built-in accounts. A
	// self-hosted installation's Organization comes from its configuration.
	if cfg.Mode() == config.DeploymentModeCloud && cfg.Auth.BuiltinAccounts {
		a.services.onboarding = a.onboarding
	}
	return a, close, nil
}

// wireOutbox publishes the domain events services record. The relay reads the
// control-plane database, so it belongs to serve, and it hands each event to
// the Redis stream through the transport-neutral events.Handler seam (ADR
// 0011).
func (a *app) wireOutbox(cfg *config.Config, logger logging.Logger) error {
	publisher, err := redisstream.NewPublisher(redisstream.Config{
		Client: a.redis,
		Stream: cfg.Outbox.Stream,
	})
	if err != nil {
		return fmt.Errorf("outbox publisher: %w", err)
	}
	a.relay, err = outbox.NewRelay(outbox.RelayConfig{
		Store:   a.outbox,
		Handler: events.Forward(publisher),
		Logger:  logger,
	})
	if err != nil {
		return fmt.Errorf("outbox relay: %w", err)
	}
	logger.Info("outbox relay configured", logging.F("stream", cfg.Outbox.Stream))
	return nil
}

// wireWebhooks applies the normalized provider events the gateway publishes.
// Each delivery is handled in one transaction, so the delivery record and the
// change commit together.
func (a *app) wireWebhooks(cfg *config.Config, logger logging.Logger) error {
	connections := versioncontrolpg.NewConnectionRepository(a.db)
	handler, err := webhooks.NewHandler(
		webhooks.WithTransactor(a.tx),
		webhooks.WithConnectionRepo(connections),
		webhooks.WithSourceRepo(versioncontrolpg.NewSourceRepository(a.db)),
		webhooks.WithDeliveryRepo(versioncontrolpg.NewWebhookDeliveryRepository(a.db)),
		webhooks.WithOutbox(a.outbox),
		webhooks.WithAudit(a.audit),
		webhooks.WithLogger(logger),
	)
	if err != nil {
		return err
	}

	var router events.Router
	events.On(&router, handler.HandleRepositoryChanged)
	events.On(&router, handler.HandleInstallationChanged)

	// The gateway resolves route keys as a platform workload (ADR 0015). The
	// lookup is always served; with no platform gateway configured, no one
	// is allowed to call it.
	lookup, err := webhookroutes.New(connections, a.verifier, a.authz, logger)
	if err != nil {
		return err
	}
	a.handlers = append(a.handlers, lookup)

	consumerName := cfg.Events.ConsumerName
	if consumerName == "" {
		consumerName, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("consumer name: %w", err)
		}
	}
	a.events, err = redisstream.NewConsumer(redisstream.ConsumerConfig{
		Client:           a.redis,
		Stream:           cfg.Events.Stream,
		Group:            cfg.Events.ConsumerGroup,
		Consumer:         consumerName,
		Handler:          &router,
		ManualAck:        false,
		Batch:            100,
		Concurrency:      cfg.Events.Concurrency,
		BlockTimeout:     cfg.Events.BlockTimeout,
		ClaimIdleTimeout: cfg.Events.ClaimIdleTimeout,
		MaxDeliveries:    cfg.Events.MaxDeliveries,
		DeadLetterStream: cfg.Events.DeadLetterStream,
		Logger:           logger,
	})
	if err != nil {
		return fmt.Errorf("webhook consumer: %w", err)
	}
	logger.Info("webhook consumer configured",
		logging.F("stream", cfg.Events.Stream),
		logging.F("group", cfg.Events.ConsumerGroup),
		logging.F("consumer", consumerName),
	)
	return nil
}

// wireProfile serves GET /v1/me, which tells a signed-in caller who they are.
func (a *app) wireProfile() error {
	svc, err := profile.NewProfileService(
		profile.WithPrincipalRepo(iampg.NewPrincipalRepository(a.db)),
		profile.WithUserRepo(a.iam.users),
		profile.WithAccountRepo(a.iam.accounts),
		profile.WithOrganizationRepo(a.iam.orgs),
		profile.WithAccessRepo(iampg.NewAccessRepository(a.db)),
		profile.WithAuthorizer(a.authz),
	)
	if err != nil {
		return err
	}
	a.services.profile = svc
	return nil
}

// wireOrganization serves GET and PATCH /v1/organization, the caller's
// Organization.
func (a *app) wireOrganization() error {
	svc, err := organizations.NewOrganizationService(
		organizations.WithOrganizationRepo(a.iam.orgs),
		organizations.WithAuthorizer(a.authz),
		organizations.WithTransactor(a.tx),
		organizations.WithOutboxRecorder(a.outbox),
		organizations.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.organizations = svc
	return nil
}

// wireAccessAPI serves who is in the Organization and what they may do:
// GET /v1/people, GET /v1/principals, and granting and removing roles at
// /v1/grants.
func (a *app) wireAccessAPI() error {
	people, err := peopleapi.NewPeopleService(
		peopleapi.WithPeopleRepo(iampg.NewPeopleRepository(a.db)),
		peopleapi.WithAuthorizer(a.authz),
	)
	if err != nil {
		return err
	}
	principalTeams, err := team.NewPrincipalTeamSource(a.teams)
	if err != nil {
		return err
	}
	principals, err := principalsapi.NewPrincipalService(
		principalsapi.WithPrincipalReader(iampg.NewPrincipalReader(a.db)),
		principalsapi.WithPrincipalTeamSource(principalTeams),
		principalsapi.WithAuthorizer(a.authz),
	)
	if err != nil {
		return err
	}
	// Roles granted to a Team go to the group that mirrors it, which the
	// team capability keeps.
	teamGroups, err := team.NewGroupSource(teampg.NewTeamRepository(a.db), iampg.NewTeamGroups(a.db))
	if err != nil {
		return err
	}
	grantSvc, err := grants.NewGrantService(
		grants.WithGrantRepo(a.iam.grants),
		grants.WithResourceRepo(iampg.NewResourceRepository(a.db)),
		grants.WithPrincipalRepo(iampg.NewPrincipalRepository(a.db)),
		grants.WithOrganizationRepo(a.iam.orgs),
		grants.WithTeamGroupSource(teamGroups),
		grants.WithAuthorizer(a.authz),
		grants.WithTransactor(a.tx),
		grants.WithOutboxRecorder(a.outbox),
		grants.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.people, a.services.principals, a.services.grants = people, principals, grantSvc
	return nil
}

// wireAgents serves /v1/agents: Agent identity, Team memberships, and the
// orchestration-owned system prompt.
func (a *app) wireAgents() error {
	svc, err := agents.NewService(
		agents.WithAgentRepo(iampg.NewAgentRepository(a.db)),
		agents.WithConfigRepo(orchestrationpg.NewAgentConfigRepository(a.db)),
		agents.WithTeams(a.services.teams),
		agents.WithTeamDirectory(a.teams),
		agents.WithAuthorizer(a.authz),
		agents.WithTransactor(a.tx),
		agents.WithOutboxRecorder(a.outbox),
		agents.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.agents = svc
	return nil
}

// wireServiceAccounts serves service accounts and their API tokens.
func (a *app) wireServiceAccounts() error {
	svc, err := serviceaccounts.NewService(
		serviceaccounts.WithServiceAccountRepo(iampg.NewServiceAccountRepository(a.db)),
		serviceaccounts.WithAPITokenRepo(iampg.NewAPITokenRepository(a.db)),
		serviceaccounts.WithAuthorizer(a.authz),
		serviceaccounts.WithTransactor(a.tx),
		serviceaccounts.WithOutboxRecorder(a.outbox),
		serviceaccounts.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.serviceAccounts = svc
	return nil
}

// wireEnrollment serves Cluster enrolment and workload credential renewal.
func (a *app) wireEnrollment(runners config.Runners, externalURL string, logger logging.Logger) error {
	svc, err := enrollment.NewService(
		enrollment.WithWorkloadRepo(iampg.NewWorkloadRepository(a.db)),
		enrollment.WithEnrollmentTokenRepo(iampg.NewWorkloadEnrollmentTokenRepository(a.db)),
		enrollment.WithCredentialRepo(iampg.NewWorkloadCredentialRepository(a.db)),
		enrollment.WithPrincipalRepo(iampg.NewPrincipalRepository(a.db)),
		enrollment.WithClusters(deliverypg.NewClusterRepository(a.db)),
		enrollment.WithKubernetesVerifier(a.kubernetes),
		enrollment.WithAuthorizer(a.authz),
		enrollment.WithTransactor(a.tx),
		enrollment.WithOutboxRecorder(a.outbox),
		enrollment.WithAuditRecorder(a.audit),
		enrollment.WithEnrollmentTokenTTL(runners.EnrollmentTokenTTL),
		enrollment.WithCredentialTTL(runners.WorkloadCredentialTTL),
		enrollment.WithLogger(logger),
		enrollment.WithInstall(a.enrollmentInstall(runners, externalURL)),
	)
	if err != nil {
		return err
	}
	a.services.enrollment = svc
	return nil
}

// enrollmentInstall is what the onboarding guide shows to connect a cluster:
// where the control plane is, which agent chart to install, and how the agent
// may authenticate.
func (a *app) enrollmentInstall(runners config.Runners, externalURL string) enrollment.InstallDetails {
	version := runners.AgentChartVersion
	if version == "" {
		version = agentChartVersion(AGENT_CHART_VERSION)
	}
	kinds := []string{"bearer"}
	if len(runners.KubernetesIssuers) > 0 {
		kinds = append(kinds, "kubernetes")
	}
	return enrollment.InstallDetails{
		ServerURL: externalURL, Chart: runners.AgentChart, ChartVersion: version, CredentialKinds: kinds,
	}
}

// releaseVersion is SemVer 2.0 with an optional leading "v", as git describe
// prints a tag.
var releaseVersion = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// agentChartVersion is the released agent chart this control plane was built
// against, or "" when none is known: "0.0.0" is the placeholder for a build
// made before the agent's first release, and a value that is not a released
// SemVer names no published chart. The guide then prints no version at all
// rather than one Helm cannot find.
//
// It is not the control plane's own version: the components are released on
// their own schedules, so after the first divergence the two never match.
func agentChartVersion(pin string) string {
	pin = strings.TrimSpace(pin)
	if pin == "" || pin == "0.0.0" || !releaseVersion.MatchString(pin) || strings.HasSuffix(pin, "-dirty") {
		return ""
	}
	// A git describe string names commits after a tag, not a published chart.
	if describedVersion.MatchString(pin) {
		return ""
	}
	return strings.TrimPrefix(pin, "v")
}

// describedVersion matches the "-3-gabc1234" part of `git describe` output,
// which names no published artifact.
var describedVersion = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+`)

// wireSignIn builds sign-in, the authorization server, and its HTTP side.
func (a *app) wireSignIn(cfg *config.Config, logger logging.Logger, passwords hasher.PasswordHasher) error {
	adminEmail := ""
	if cfg.Mode() == config.DeploymentModeSelfHosted {
		adminEmail = cfg.Bootstrap.AdminEmail
	}
	var err error
	a.auth, err = signin.New(
		signin.WithLogger(logger),
		signin.WithOrganizationRepo(a.iam.orgs),
		signin.WithAccountRepo(a.iam.accounts),
		signin.WithPasswordCredentialRepo(a.iam.passwords),
		signin.WithUserRepo(a.iam.users),
		signin.WithIdentityProviderRepo(a.iam.providers),
		signin.WithExternalIdentityRepo(a.iam.externals),
		signin.WithAccountIdentityRepo(a.iam.platform),
		signin.WithGrantRepo(a.iam.grants),
		signin.WithPasswordHasher(passwords),
		signin.WithTransactor(a.tx),
		signin.WithOutboxRecorder(a.outbox),
		signin.WithBuiltinAccounts(cfg.Auth.BuiltinAccounts),
		signin.WithBootstrapAdminEmail(adminEmail),
	)
	if err != nil {
		return err
	}

	issuer, err := token.NewIssuer(a.keys, cfg.IssuerURL(), cfg.Auth.AccessTokenTTL)
	if err != nil {
		return err
	}
	a.oauth, err = oauthserver.New(
		oauthserver.WithSettings(oauthserver.Settings{
			Issuer:     cfg.IssuerURL(),
			Resources:  []string{cfg.IssuerURL()},
			CodeTTL:    cfg.Auth.AuthorizationCodeTTL,
			RefreshTTL: cfg.Auth.RefreshTokenTTL,
			AccessTTL:  cfg.Auth.AccessTokenTTL,
		}),
		oauthserver.WithLogger(logger),
		oauthserver.WithIssuer(issuer),
		oauthserver.WithAuthentication(a.auth),
		oauthserver.WithOAuthClientRepo(a.iam.clients),
		oauthserver.WithAuthorizationCodeRepo(a.iam.codes),
		oauthserver.WithRefreshTokenRepo(a.iam.refreshes),
		oauthserver.WithUserRepo(a.iam.users),
		oauthserver.WithTransactor(a.tx),
		oauthserver.WithOutboxRecorder(a.outbox),
	)
	if err != nil {
		return err
	}

	providers, err := installationProviders(cfg, a.install)
	if err != nil {
		return err
	}
	platform, err := platformProviders(cfg)
	if err != nil {
		return err
	}
	providers = append(providers, platform...)
	var extra []authhttp.Option
	if cfg.Mode() == config.DeploymentModeCloud {
		extra = append(extra, authhttp.WithOnboarding(a.onboarding))
	}
	// GitHub may return from an App install to the sign-in callback, when the
	// App also signs people in; send those returns on to the web app.
	if page := cfg.GitHubInstallCallbackURL(); cfg.VersionControl.GitHub.Configured() && page != "" {
		extra = append(extra, authhttp.WithInstallReturns(map[string]string{"github": page}))
	}

	cookieKeys, err := a.keys.DeriveKeys(session.KeyPurpose)
	if err != nil {
		return err
	}
	codec, err := session.NewCodec(cookieKeys)
	if err != nil {
		return err
	}
	authServer, err := authhttp.New(append([]authhttp.Option{
		authhttp.WithSettings(authhttp.Settings{
			Issuer:          cfg.IssuerURL(),
			LoginURL:        cfg.Auth.LoginURL,
			SessionTTL:      cfg.Auth.SignInSessionTTL,
			BuiltinAccounts: cfg.Auth.BuiltinAccounts,
		}),
		authhttp.WithLogger(logger),
		authhttp.WithAuthentication(a.auth),
		authhttp.WithAuthorizationServer(a.oauth),
		authhttp.WithCodec(codec),
		authhttp.WithProviders(providers...),
	}, extra...)...)
	if err != nil {
		return err
	}
	a.handlers = append(a.handlers, authServer)
	a.services.signIn = authServer
	return nil
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
