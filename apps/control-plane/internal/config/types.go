package config

import "time"

// Config represents the configuration for the control plane server.
//
// Secrets are never configured as values: fields that need one take the path
// of a file holding it (a mounted Kubernetes secret, for example), so secrets
// stay out of YAML files, the process environment, and logs.
type Config struct {
	DeploymentMode string       `yaml:"deployment_mode" env:"IVALDI_DEPLOYMENT_MODE" envDefault:"self-hosted"`
	Port           *int         `yaml:"port" env:"IVALDI_PORT" envDefault:"8660"`
	Logging        *Logging     `yaml:"logging"`
	Persistence    *Persistence `yaml:"persistence"`

	// Host is the interface the API listens on; empty means all of them.
	Host string `yaml:"host" env:"IVALDI_HOST"`
	// ExternalURL is the address clients use to reach the API, such as
	// https://ivaldi.example.com. It is the OAuth issuer, the base of the
	// published signing keys and protected-resource metadata, and the base
	// of identity-provider redirect URIs, so it must be what clients see,
	// not the listen address behind a proxy.
	ExternalURL string `yaml:"external_url" env:"IVALDI_EXTERNAL_URL" envDefault:"http://localhost:8660"`
	// ShutdownTimeout bounds how long in-flight requests and runner
	// connections get to finish when the process is asked to stop.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"IVALDI_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	// TLS serves the API over TLS. Leave it empty when a proxy or load
	// balancer terminates TLS in front of the control plane.
	TLS TLS `yaml:"tls"`

	Runners    Runners    `yaml:"runners"`
	Auth       Auth       `yaml:"auth"`
	Bootstrap  Bootstrap  `yaml:"bootstrap"`
	Encryption Encryption `yaml:"encryption"`
	Redis      Redis      `yaml:"redis"`
	Events     Events     `yaml:"events"`
	Platform   Platform   `yaml:"platform"`
	Outbox     Outbox     `yaml:"outbox"`
	// Notifications configures how the control plane tells people about
	// work that waits for them, such as approval requests.
	Notifications Notifications `yaml:"notifications"`
	Telemetry     Telemetry     `yaml:"telemetry"`

	// VersionControl configures the version-control providers Ivaldi can
	// read repositories and commit history from.
	VersionControl VersionControl `yaml:"version_control"`

	// SecretStores configures how the control plane grants runtime agents
	// access to Secret Stores (ADR 0017).
	SecretStores SecretStores `yaml:"secret_stores"`
}

// SecretStores configures the control plane's access to Secret Stores.
type SecretStores struct {
	Federation Federation `yaml:"federation"`
}

// Federation makes the control plane an OIDC issuer for Secret Stores in
// federated mode, at ExternalURL + "/federation". Without signing keys, no
// store can be in that mode.
type Federation struct {
	// SigningKeyFiles are PEM private keys, as for auth.signing_key_files,
	// and never the same ones. The first signs; all are published, so a key
	// is rotated by putting a new one first and removing the old one once
	// the JWTs it signed, which last five minutes, have expired.
	SigningKeyFiles []string `yaml:"signing_key_files" env:"IVALDI_SECRET_STORES_FEDERATION_SIGNING_KEY_FILES"`
}

// DeploymentMode is who operates the control plane (ADR 0001). A managed
// cloud with bring-your-own-compute is DeploymentModeCloud: from the control
// plane's side, only where its runners connect from differs.
type DeploymentMode string

const (
	DeploymentModeSelfHosted DeploymentMode = "self-hosted"
	DeploymentModeCloud      DeploymentMode = "cloud"
)

// Logging represents the logging configuration for the control plane server.
type Logging struct {
	Level  *string `yaml:"level" env:"IVALDI_LOG_LEVEL" envDefault:"info"`
	Output *string `yaml:"output" env:"IVALDI_LOG_OUTPUT" envDefault:"console"`
	Format *string `yaml:"format" env:"IVALDI_LOG_FORMAT" envDefault:"json"`
	// Directory, FilePrefix, MaxFileSizeMB, and MaxAgeDays apply only when
	// Output is "file".
	Directory     string `yaml:"directory" env:"IVALDI_LOG_DIRECTORY" envDefault:"logs"`
	FilePrefix    string `yaml:"file_prefix" env:"IVALDI_LOG_FILE_PREFIX" envDefault:"ivaldi-control-plane"`
	MaxFileSizeMB int    `yaml:"max_file_size_mb" env:"IVALDI_LOG_MAX_FILE_SIZE_MB" envDefault:"50"`
	MaxAgeDays    int    `yaml:"max_age_days" env:"IVALDI_LOG_MAX_AGE_DAYS" envDefault:"7"`
}

// Persistence represents the persistence configuration for the control plane server.
type Persistence struct {
	DatabaseURL           *string       `yaml:"database_url" env:"IVALDI_DATABASE_URL"`
	DatabaseHost          *string       `yaml:"database_host" env:"IVALDI_DATABASE_HOST"`
	DatabasePort          *int          `yaml:"database_port" env:"IVALDI_DATABASE_PORT"`
	DatabaseUser          *string       `yaml:"database_user" env:"IVALDI_DATABASE_USER"`
	DatabasePassword      *string       `yaml:"database_password" env:"IVALDI_DATABASE_PASSWORD"`
	DatabaseName          *string       `yaml:"database_name" env:"IVALDI_DATABASE_NAME" envDefault:"ivaldi"`
	MaxPoolSize           int           `yaml:"max_pool_size" env:"IVALDI_DATABASE_MAX_POOL_SIZE" envDefault:"10"`
	MinPoolSize           int           `yaml:"min_pool_size" env:"IVALDI_DATABASE_MIN_POOL_SIZE" envDefault:"1"`
	ConnectionIdleTimeout time.Duration `yaml:"connection_idle_timeout" env:"IVALDI_DATABASE_CONNECTION_IDLE_TIMEOUT" envDefault:"120s"`
	ConnectionTimeout     time.Duration `yaml:"connection_timeout" env:"IVALDI_DATABASE_CONNECTION_TIMEOUT" envDefault:"15s"`
	SSLMode               *string       `yaml:"ssl_mode" env:"IVALDI_DATABASE_SSL_MODE" envDefault:"disable"`
	// MigrateOnStart applies each capability's pending migrations before the
	// server starts. Leave it off where several replicas start at once, and
	// run migrations as a separate step instead.
	MigrateOnStart bool `yaml:"migrate_on_start" env:"IVALDI_DATABASE_MIGRATE_ON_START" envDefault:"false"`
}

// TLS is a certificate and key for a listener, as PEM files. ClientCAFile
// enables optional client-certificate authentication for workloads.
type TLS struct {
	CertFile     string `yaml:"cert_file" env:"IVALDI_TLS_CERT_FILE"`
	KeyFile      string `yaml:"key_file" env:"IVALDI_TLS_KEY_FILE"`
	ClientCAFile string `yaml:"client_ca_file" env:"IVALDI_TLS_CLIENT_CA_FILE"`
}

// Runners configures the endpoint data-plane runners (workers, indexers,
// gateways, and runtime agents) connect out to and receive work over
// (ADR 0001). It is a separate listener from the API, because runners
// authenticate with mutual TLS after they enroll.
type Runners struct {
	Host string `yaml:"host" env:"IVALDI_RUNNER_HOST"`
	Port int    `yaml:"port" env:"IVALDI_RUNNER_PORT" envDefault:"8661"`
	// CertFile and KeyFile are the listener's own certificate.
	CertFile string `yaml:"cert_file" env:"IVALDI_RUNNER_TLS_CERT_FILE"`
	KeyFile  string `yaml:"key_file" env:"IVALDI_RUNNER_TLS_KEY_FILE"`
	// ClientCAFile verifies the workload certificates runners present after
	// enrolling. Kubernetes runners that use projected service-account
	// tokens instead are verified through IAM.
	ClientCAFile string `yaml:"client_ca_file" env:"IVALDI_RUNNER_CLIENT_CA_FILE"`
	// EnrollmentTokenTTL is how long a one-time enrollment token is valid.
	EnrollmentTokenTTL time.Duration `yaml:"enrollment_token_ttl" env:"IVALDI_RUNNER_ENROLLMENT_TOKEN_TTL" envDefault:"1h"`
	// WorkloadCredentialTTL is how long an issued workload credential is
	// valid before the runner must rotate it.
	WorkloadCredentialTTL time.Duration `yaml:"workload_credential_ttl" env:"IVALDI_RUNNER_WORKLOAD_CREDENTIAL_TTL" envDefault:"24h"`
	// HeartbeatInterval is how often a connected runner must report in;
	// one silent for three intervals is treated as offline.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" env:"IVALDI_RUNNER_HEARTBEAT_INTERVAL" envDefault:"30s"`
	// AgentChart is the OCI reference of the ivaldi-agent Helm chart the
	// onboarding guide installs; a mirror overrides it. AgentChartVersion is
	// the chart version matching this control plane, and defaults to its own
	// version.
	AgentChart        string `yaml:"agent_chart" env:"IVALDI_RUNNER_AGENT_CHART" envDefault:"oci://ghcr.io/ivaldibuild/charts/ivaldi-agent"`
	AgentChartVersion string `yaml:"agent_chart_version" env:"IVALDI_RUNNER_AGENT_CHART_VERSION"`
	// AgentPollInterval and AgentWatchInterval are the pace the control plane
	// tells a Cluster's agent to poll assignments and refresh its Targets.
	AgentPollInterval  time.Duration `yaml:"agent_poll_interval" env:"IVALDI_RUNNER_AGENT_POLL_INTERVAL" envDefault:"5s"`
	AgentWatchInterval time.Duration `yaml:"agent_watch_interval" env:"IVALDI_RUNNER_AGENT_WATCH_INTERVAL" envDefault:"60s"`
	// KubernetesIssuers are the OIDC issuers whose projected service-account
	// tokens Ivaldi accepts. Empty disables that credential kind.
	KubernetesIssuers []string `yaml:"kubernetes_issuers" env:"IVALDI_RUNNER_KUBERNETES_ISSUERS"`
	// KubernetesAudience is the audience a projected token must contain.
	KubernetesAudience string `yaml:"kubernetes_audience" env:"IVALDI_RUNNER_KUBERNETES_AUDIENCE" envDefault:"ivaldi"`
}

// Auth configures the control plane as the OAuth 2.1 authorization server
// and token issuer (ADR 0003), and a self-hosted installation's sign-in
// providers.
type Auth struct {
	// SigningKeyFiles are PEM private keys for signing tokens. The first
	// signs; all are published in the JWKS, so a key can be rotated out by
	// moving a new one to the front and dropping the old one once the
	// tokens it signed have expired.
	SigningKeyFiles []string `yaml:"signing_key_files" env:"IVALDI_AUTH_SIGNING_KEY_FILES"`
	// EphemeralSigningKey signs tokens with a key generated at start, when no
	// SigningKeyFiles are set. Its tokens stop working when the process
	// restarts, and other replicas cannot verify them, so it is only for
	// local development.
	EphemeralSigningKey bool `yaml:"ephemeral_signing_key" env:"IVALDI_AUTH_EPHEMERAL_SIGNING_KEY" envDefault:"false"`
	// AccessTokenTTL bounds how long a revoked grant keeps working, because
	// tokens are verified offline.
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env:"IVALDI_AUTH_ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env:"IVALDI_AUTH_REFRESH_TOKEN_TTL" envDefault:"720h"`
	// SessionCredentialTTL is the lifetime of the short-lived credentials
	// agent Sessions receive.
	SessionCredentialTTL time.Duration `yaml:"session_credential_ttl" env:"IVALDI_AUTH_SESSION_CREDENTIAL_TTL" envDefault:"15m"`
	// AuthorizationCodeTTL is how long an OAuth authorization code may wait
	// to be exchanged.
	AuthorizationCodeTTL time.Duration `yaml:"authorization_code_ttl" env:"IVALDI_AUTH_AUTHORIZATION_CODE_TTL" envDefault:"1m"`
	// SignInSessionTTL is how long a browser stays signed in to the
	// authorization server, between signing in and being sent back to the
	// client that asked. Clients keep people signed in with refresh tokens.
	SignInSessionTTL time.Duration `yaml:"sign_in_session_ttl" env:"IVALDI_AUTH_SIGN_IN_SESSION_TTL" envDefault:"10m"`
	// BuiltinAccounts turns on Ivaldi's own username and password accounts,
	// for small self-hosted installations without an identity provider.
	BuiltinAccounts bool `yaml:"builtin_accounts" env:"IVALDI_AUTH_BUILTIN_ACCOUNTS" envDefault:"false"`
	// LoginURL is the frontend's sign-in page. The authorization server
	// sends people there with a login_challenge, and the frontend signs them
	// in through the control plane's /v1/auth endpoints; the control plane
	// serves no sign-in pages of its own.
	LoginURL string `yaml:"login_url" env:"IVALDI_AUTH_LOGIN_URL"`
	// WebClient is Ivaldi's web app, which signs people in as a first-party
	// OAuth client.
	WebClient WebClient `yaml:"web_client"`
	// Providers are the self-hosted installation's sign-in providers.
	Providers Providers `yaml:"providers"`
	// Platform are the managed cloud's sign-up and sign-in providers.
	Platform PlatformProviders `yaml:"platform"`
}

// WebClient configures the web app as a first-party OAuth client. Leaving
// RedirectURIs empty leaves the web app unregistered.
type WebClient struct {
	// RedirectURIs are where the authorization server may send the browser
	// back to the web app, such as https://app.example.com/auth/callback.
	RedirectURIs []string `yaml:"redirect_uris" env:"IVALDI_AUTH_WEB_CLIENT_REDIRECT_URIS"`
}

// Bootstrap creates the first Organization and its first administrator when
// a self-hosted control plane starts with an empty database. It is ignored
// once an Organization exists. The managed cloud creates Organizations
// through sign-up instead.
type Bootstrap struct {
	OrganizationSlug string `yaml:"organization_slug" env:"IVALDI_BOOTSTRAP_ORGANIZATION_SLUG"`
	OrganizationName string `yaml:"organization_name" env:"IVALDI_BOOTSTRAP_ORGANIZATION_NAME"`
	// AdminEmail is the user made the Organization's first administrator
	// when they first sign in, through any enabled provider.
	AdminEmail string `yaml:"admin_email" env:"IVALDI_BOOTSTRAP_ADMIN_EMAIL"`
	// AdminPasswordFile holds a password for AdminEmail, so the administrator
	// can sign in with a built-in account before any provider is set up. It
	// needs BuiltinAccounts, and is used once: it never replaces a password.
	AdminPasswordFile string `yaml:"admin_password_file" env:"IVALDI_BOOTSTRAP_ADMIN_PASSWORD_FILE"`
}

// IdentityProviderKind is how people sign in to an Organization.
type IdentityProviderKind string

const (
	// IdentityProviderGoogle is Google sign-in, through OIDC.
	IdentityProviderGoogle IdentityProviderKind = "google"
	// IdentityProviderGitHub is GitHub sign-in, through GitHub's OAuth flow
	// and user API, because GitHub issues no ID token.
	IdentityProviderGitHub IdentityProviderKind = "github"
	// IdentityProviderOIDC is any OpenID Connect provider, such as Keycloak,
	// Dex, Entra ID, or Okta.
	IdentityProviderOIDC IdentityProviderKind = "oidc"
)

// Providers are a self-hosted installation's sign-in providers. Any of them
// can be enabled at once, and a provider is enabled by setting its client ID
// and secret, which come from the OAuth app the installation's operator
// registers with the provider. The redirect URI to register there is
// ExternalURL + "/auth/oauth/<kind>/callback", for example
// https://ivaldi.example.com/auth/oauth/github/callback.
//
// The control plane applies them on every start, as identity providers of
// the installation's Organization: configuration is their source of truth,
// so they cannot be edited through the API, and removing one from the
// configuration disables it. In the managed cloud, Organizations configure
// their own identity providers in IAM, and these are not used.
//
// Client secrets are read from the environment variable or from a file,
// never from YAML. Use the _FILE variable to read a mounted secret.
type Providers struct {
	Google GoogleProvider `yaml:"google"`
	GitHub GitHubProvider `yaml:"github"`
	OIDC   OIDCProvider   `yaml:"oidc"`
}

// GoogleProvider signs people in with Google. Google accepts any account in
// the world, so it needs AllowedDomains or AllowedEmails, and is refused
// without either.
type GoogleProvider struct {
	ClientID         string `yaml:"client_id" env:"IVALDI_AUTH_GOOGLE_CLIENT_ID"`
	ClientSecret     Secret `yaml:"-" env:"IVALDI_AUTH_GOOGLE_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_AUTH_GOOGLE_CLIENT_SECRET_FILE"`
	// AllowedDomains admits Google Workspace domains, matched against the ID
	// token's hd claim, so consumer Gmail accounts never match.
	AllowedDomains []string `yaml:"allowed_domains" env:"IVALDI_AUTH_GOOGLE_ALLOWED_DOMAINS"`
	// AllowedEmails admits these verified email addresses, including Gmail ones.
	AllowedEmails []string `yaml:"allowed_emails" env:"IVALDI_AUTH_GOOGLE_ALLOWED_EMAILS"`
	// JITProvisioning makes anyone the provider admits a member on their
	// first sign-in. With it off, only the bootstrap administrator can join.
	JITProvisioning *bool `yaml:"jit_provisioning" env:"IVALDI_AUTH_GOOGLE_JIT_PROVISIONING" envDefault:"true"`
}

// GitHubProvider signs people in with GitHub, through a GitHub OAuth app or
// the user authorization of the GitHub App that connects Sources. GitHub
// accepts any account in the world, so it needs AllowedOrganizations or
// AllowedEmails, and is refused without either.
type GitHubProvider struct {
	ClientID         string `yaml:"client_id" env:"IVALDI_AUTH_GITHUB_CLIENT_ID"`
	ClientSecret     Secret `yaml:"-" env:"IVALDI_AUTH_GITHUB_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_AUTH_GITHUB_CLIENT_SECRET_FILE"`
	// URL is the GitHub to sign in with. Set it for GitHub Enterprise Server.
	URL string `yaml:"url" env:"IVALDI_AUTH_GITHUB_URL" envDefault:"https://github.com"`
	// AllowedOrganizations admits members of these organizations, by login.
	// Checking membership needs the read:org scope, and an organization may
	// have to approve the OAuth app before its members can sign in.
	AllowedOrganizations []string `yaml:"allowed_organizations" env:"IVALDI_AUTH_GITHUB_ALLOWED_ORGANIZATIONS"`
	// AllowedEmails admits these email addresses, when GitHub marks them verified.
	AllowedEmails []string `yaml:"allowed_emails" env:"IVALDI_AUTH_GITHUB_ALLOWED_EMAILS"`
	// JITProvisioning makes anyone the provider admits a member on their
	// first sign-in. With it off, only the bootstrap administrator can join.
	JITProvisioning *bool `yaml:"jit_provisioning" env:"IVALDI_AUTH_GITHUB_JIT_PROVISIONING" envDefault:"true"`
}

// OIDCProvider signs people in with any OpenID Connect provider. Its
// restrictions are optional, because such a provider is usually the
// installation's own.
type OIDCProvider struct {
	IssuerURL        string `yaml:"issuer_url" env:"IVALDI_AUTH_OIDC_ISSUER_URL"`
	ClientID         string `yaml:"client_id" env:"IVALDI_AUTH_OIDC_CLIENT_ID"`
	ClientSecret     Secret `yaml:"-" env:"IVALDI_AUTH_OIDC_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_AUTH_OIDC_CLIENT_SECRET_FILE"`
	// DisplayName labels its sign-in button, such as "Okta".
	DisplayName string `yaml:"display_name" env:"IVALDI_AUTH_OIDC_DISPLAY_NAME" envDefault:"Single sign-on"`
	// Scopes are requested in addition to openid.
	Scopes         []string `yaml:"scopes" env:"IVALDI_AUTH_OIDC_SCOPES" envDefault:"email,profile"`
	AllowedDomains []string `yaml:"allowed_domains" env:"IVALDI_AUTH_OIDC_ALLOWED_DOMAINS"`
	AllowedEmails  []string `yaml:"allowed_emails" env:"IVALDI_AUTH_OIDC_ALLOWED_EMAILS"`
	// JITProvisioning makes anyone the provider admits a member on their
	// first sign-in. With it off, only the bootstrap administrator can join.
	JITProvisioning *bool `yaml:"jit_provisioning" env:"IVALDI_AUTH_OIDC_JIT_PROVISIONING" envDefault:"true"`
}

// PlatformProviders let people sign up for and sign in to the managed cloud
// with an existing account, before they belong to any Organization. They are
// the platform's own OAuth apps, not an Organization's identity providers,
// and are only used in DeploymentModeCloud. The redirect URI to register is
// ExternalURL + "/auth/oauth/<kind>/callback", as for Providers.
type PlatformProviders struct {
	Google PlatformGoogleProvider `yaml:"google"`
	GitHub PlatformGitHubProvider `yaml:"github"`
}

// PlatformGoogleProvider is the managed cloud's Google sign-in.
type PlatformGoogleProvider struct {
	ClientID         string `yaml:"client_id" env:"IVALDI_AUTH_PLATFORM_GOOGLE_CLIENT_ID"`
	ClientSecret     Secret `yaml:"-" env:"IVALDI_AUTH_PLATFORM_GOOGLE_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_AUTH_PLATFORM_GOOGLE_CLIENT_SECRET_FILE"`
}

// PlatformGitHubProvider is the managed cloud's GitHub sign-in.
type PlatformGitHubProvider struct {
	ClientID         string `yaml:"client_id" env:"IVALDI_AUTH_PLATFORM_GITHUB_CLIENT_ID"`
	ClientSecret     Secret `yaml:"-" env:"IVALDI_AUTH_PLATFORM_GITHUB_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_AUTH_PLATFORM_GITHUB_CLIENT_SECRET_FILE"`
}

// Enabled reports whether the provider has a client ID.
func (p GoogleProvider) Enabled() bool         { return p.ClientID != "" }
func (p GitHubProvider) Enabled() bool         { return p.ClientID != "" }
func (p OIDCProvider) Enabled() bool           { return p.ClientID != "" }
func (p PlatformGoogleProvider) Enabled() bool { return p.ClientID != "" }
func (p PlatformGitHubProvider) Enabled() bool { return p.ClientID != "" }

// JIT reports whether the provider provisions members on first sign-in,
// which it does unless JITProvisioning is set to false.
func (p GoogleProvider) JIT() bool { return jit(p.JITProvisioning) }
func (p GitHubProvider) JIT() bool { return jit(p.JITProvisioning) }
func (p OIDCProvider) JIT() bool   { return jit(p.JITProvisioning) }

func jit(v *bool) bool { return v == nil || *v }

// Secret returns the client secret from its variable or its file.
func (p GoogleProvider) Secret() (Secret, error) {
	return resolveSecret("google client secret", p.ClientSecret, p.ClientSecretFile)
}

func (p GitHubProvider) Secret() (Secret, error) {
	return resolveSecret("github client secret", p.ClientSecret, p.ClientSecretFile)
}

func (p OIDCProvider) Secret() (Secret, error) {
	return resolveSecret("oidc client secret", p.ClientSecret, p.ClientSecretFile)
}

func (p PlatformGoogleProvider) Secret() (Secret, error) {
	return resolveSecret("platform google client secret", p.ClientSecret, p.ClientSecretFile)
}

func (p PlatformGitHubProvider) Secret() (Secret, error) {
	return resolveSecret("platform github client secret", p.ClientSecret, p.ClientSecretFile)
}

// EncryptionProvider is the key-management service that protects tenant
// data keys (ADR 0004).
type EncryptionProvider string

const (
	// EncryptionLocal keeps the key-encryption key in a local file, for
	// self-hosted installations without an external KMS.
	EncryptionLocal        EncryptionProvider = "local"
	EncryptionAWSKMS       EncryptionProvider = "aws-kms"
	EncryptionVaultTransit EncryptionProvider = "vault-transit"
)

// Encryption configures envelope encryption of tenant data: data keys
// encrypt the data, and the provider's key encrypts the data keys. This is
// the installation's default; Organizations with customer-managed keys
// override it in IAM.
type Encryption struct {
	Provider EncryptionProvider `yaml:"provider" env:"IVALDI_ENCRYPTION_PROVIDER" envDefault:"local"`
	// LocalKeyFile holds the 32-byte key-encryption key for EncryptionLocal.
	LocalKeyFile string `yaml:"local_key_file" env:"IVALDI_ENCRYPTION_LOCAL_KEY_FILE"`
	// AWSKMSKeyID is the key ID or ARN for EncryptionAWSKMS. Credentials come
	// from the AWS default chain, such as the pod's IAM role.
	AWSKMSKeyID string `yaml:"aws_kms_key_id" env:"IVALDI_ENCRYPTION_AWS_KMS_KEY_ID"`
	AWSRegion   string `yaml:"aws_region" env:"IVALDI_ENCRYPTION_AWS_REGION"`
	// VaultAddress, VaultKeyName, and VaultTokenFile configure
	// EncryptionVaultTransit.
	VaultAddress   string `yaml:"vault_address" env:"IVALDI_ENCRYPTION_VAULT_ADDRESS"`
	VaultKeyName   string `yaml:"vault_key_name" env:"IVALDI_ENCRYPTION_VAULT_KEY_NAME"`
	VaultTokenFile string `yaml:"vault_token_file" env:"IVALDI_ENCRYPTION_VAULT_TOKEN_FILE"`
}

// Redis is the server gateway events travel through (ADR 0009).
type Redis struct {
	// Address is host:port.
	Address string `yaml:"address" env:"IVALDI_REDIS_ADDRESS" envDefault:"localhost:6379"`
	// Username is the control plane's Redis ACL user, which may read and
	// acknowledge the events stream and write the dead-letter stream. The
	// gateway has its own user, which may only add to the events stream.
	Username     string `yaml:"username" env:"IVALDI_REDIS_USERNAME"`
	PasswordFile string `yaml:"password_file" env:"IVALDI_REDIS_PASSWORD_FILE"`
	Database     int    `yaml:"database" env:"IVALDI_REDIS_DATABASE" envDefault:"0"`
	// TLS turns on TLS, verified against CAFile, or the system roots when
	// CAFile is empty.
	TLS         bool          `yaml:"tls" env:"IVALDI_REDIS_TLS" envDefault:"false"`
	CAFile      string        `yaml:"ca_file" env:"IVALDI_REDIS_CA_FILE"`
	DialTimeout time.Duration `yaml:"dial_timeout" env:"IVALDI_REDIS_DIAL_TIMEOUT" envDefault:"5s"`
}

// Events configures how the control plane consumes the gateway's events
// stream (ADR 0009).
type Events struct {
	// Stream is the stream the gateway adds events to.
	Stream string `yaml:"stream" env:"IVALDI_EVENTS_STREAM" envDefault:"ivaldi:gateway:events"`
	// ConsumerGroup is shared by every control-plane replica, so each event
	// is handled by one of them.
	ConsumerGroup string `yaml:"consumer_group" env:"IVALDI_EVENTS_CONSUMER_GROUP" envDefault:"control-plane"`
	// ConsumerName identifies this replica in the group. Empty means the
	// host name, which is the pod name on Kubernetes.
	ConsumerName string `yaml:"consumer_name" env:"IVALDI_EVENTS_CONSUMER_NAME"`
	// Concurrency is how many events this replica handles at once.
	Concurrency int `yaml:"concurrency" env:"IVALDI_EVENTS_CONCURRENCY" envDefault:"10"`
	// BlockTimeout is how long one read waits for new events.
	BlockTimeout time.Duration `yaml:"block_timeout" env:"IVALDI_EVENTS_BLOCK_TIMEOUT" envDefault:"5s"`
	// ClaimIdleTimeout is how long an event may stay unacknowledged, such as
	// when its replica crashed, before another replica claims it. It must be
	// longer than the slowest handler.
	ClaimIdleTimeout time.Duration `yaml:"claim_idle_timeout" env:"IVALDI_EVENTS_CLAIM_IDLE_TIMEOUT" envDefault:"5m"`
	// MaxDeliveries is how often an event is tried before it is moved to
	// DeadLetterStream.
	MaxDeliveries    int    `yaml:"max_deliveries" env:"IVALDI_EVENTS_MAX_DELIVERIES" envDefault:"5"`
	DeadLetterStream string `yaml:"dead_letter_stream" env:"IVALDI_EVENTS_DEAD_LETTER_STREAM" envDefault:"ivaldi:gateway:events:dead"`
}

// Outbox configures the relay that publishes the control plane's own domain
// events (ADR 0011). Its tuning follows the outbox package's defaults.
type Outbox struct {
	// Stream is the stream the relay publishes domain events to. It is
	// separate from the gateway's stream, which carries events into the
	// control plane.
	Stream string `yaml:"stream" env:"IVALDI_OUTBOX_STREAM" envDefault:"ivaldi:control-plane:events"`
}

// Notifications configures the consumer that reads the outbox stream and
// tells people about work that waits for them (docs/design/notifications.md).
// It reads with its own consumer group, so it sees every event whoever else
// reads the stream; the rest of its tuning follows Events.
type Notifications struct {
	// ConsumerGroup is shared by every control-plane replica, so each event
	// is handled by one of them.
	ConsumerGroup string `yaml:"consumer_group" env:"IVALDI_NOTIFICATIONS_CONSUMER_GROUP" envDefault:"notifications"`
	// Concurrency is how many events this replica handles at once.
	Concurrency int `yaml:"concurrency" env:"IVALDI_NOTIFICATIONS_CONCURRENCY" envDefault:"10"`
	// DeadLetterStream receives events the consumer gave up on.
	DeadLetterStream string `yaml:"dead_letter_stream" env:"IVALDI_NOTIFICATIONS_DEAD_LETTER_STREAM" envDefault:"ivaldi:control-plane:events:notifications:dead"`
	// Email sends each notification by email too. It is off while
	// SMTPAddress is empty.
	Email NotificationEmail `yaml:"email"`
}

// NotificationEmail is the SMTP server notifications are emailed through.
// Links in emails use ExternalURL, where the control plane serves the web
// app (ADR 0016).
type NotificationEmail struct {
	// SMTPAddress is the server's host:port, such as smtp.example.com:587.
	SMTPAddress string `yaml:"smtp_address" env:"IVALDI_NOTIFICATIONS_EMAIL_SMTP_ADDRESS"`
	// Username and PasswordFile authenticate with the server, when it needs it.
	Username     string `yaml:"username" env:"IVALDI_NOTIFICATIONS_EMAIL_USERNAME"`
	PasswordFile string `yaml:"password_file" env:"IVALDI_NOTIFICATIONS_EMAIL_PASSWORD_FILE"`
	// From is the sender, such as "Ivaldi <ivaldi@example.com>".
	From string `yaml:"from" env:"IVALDI_NOTIFICATIONS_EMAIL_FROM"`
	// TLS is starttls (the default), tls, or none, which sends in plain
	// text and is only allowed for a server on this host.
	TLS string `yaml:"tls" env:"IVALDI_NOTIFICATIONS_EMAIL_TLS" envDefault:"starttls"`
}

// Enabled reports whether notifications are emailed.
func (e NotificationEmail) Enabled() bool { return e.SMTPAddress != "" }

// Password returns the SMTP password from its file, or "" when none is
// configured.
func (e NotificationEmail) Password() (Secret, error) {
	return resolveSecret("notifications.email.password", "", e.PasswordFile)
}

// Telemetry configures the control plane's own health, metrics, and traces.
type Telemetry struct {
	// Port serves liveness and readiness probes and Prometheus metrics, apart
	// from the API so they need no authentication and are not exposed with it.
	Port int `yaml:"port" env:"IVALDI_TELEMETRY_PORT" envDefault:"8662"`
	// OTLPEndpoint is where OpenTelemetry traces are exported, such as
	// otel-collector:4317. Empty turns tracing off.
	OTLPEndpoint     string  `yaml:"otlp_endpoint" env:"IVALDI_TELEMETRY_OTLP_ENDPOINT"`
	TraceSampleRatio float64 `yaml:"trace_sample_ratio" env:"IVALDI_TELEMETRY_TRACE_SAMPLE_RATIO" envDefault:"0.1"`
	ServiceName      string  `yaml:"service_name" env:"IVALDI_TELEMETRY_SERVICE_NAME" envDefault:"ivaldi-control-plane"`
}
