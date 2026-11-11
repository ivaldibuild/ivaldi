// Package bootstrap brings IAM's stored state in line with the control
// plane's configuration at start: the first-party OAuth clients and platform
// workloads everywhere, and in a self-hosted installation its Organization,
// its configured identity providers, and optionally its administrator's
// password.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Bootstrapper applies the configuration. It is safe to run on every start,
// from several replicas at once.
type Bootstrapper interface {
	Bootstrap(ctx context.Context) (*Installation, error)
}

// Installation is what bootstrap found or created.
type Installation struct {
	// OrganizationID is a self-hosted installation's Organization; empty in
	// the managed cloud.
	OrganizationID string
	// Providers are the IDs of the installation's active configured
	// identity providers, by protocol.
	Providers map[iam.IdentityProtocol]string
}

// Mode is who operates the control plane.
type Mode string

const (
	ModeSelfHosted Mode = "self-hosted"
	ModeCloud      Mode = "cloud"
)

// Settings is the configuration bootstrap applies, as plain values.
type Settings struct {
	Mode Mode
	// Clients are the first-party OAuth clients, applied in every mode.
	Clients []ClientSettings
	// PlatformWorkloads are the components that serve every Organization,
	// such as the gateway (ADR 0015), applied in every mode.
	PlatformWorkloads []PlatformWorkloadSettings
	// The rest applies to a self-hosted installation only.
	Organization OrganizationSettings
	// AdminEmail becomes the Organization's owner on first sign-in.
	AdminEmail string
	// AdminPassword, when set, creates AdminEmail's account with it up front,
	// for signing in with a built-in account.
	AdminPassword string
	// Providers are the configured identity providers, including the builtin
	// one when built-in accounts are on.
	Providers []ProviderSettings
}

// ClientSettings is a first-party OAuth client.
type ClientSettings struct {
	ClientID     string
	Name         string
	RedirectURIs []string
}

// PlatformWorkloadSettings is a configured platform workload.
type PlatformWorkloadSettings struct {
	// Name identifies it among platform workloads.
	Name      string
	Component iam.WorkloadComponent
	// PublicKeys are the keys it may sign requests with, DER-encoded (PKIX).
	PublicKeys [][]byte
}

// OrganizationSettings name a self-hosted installation's Organization.
type OrganizationSettings struct {
	Slug string
	Name string
}

// ProviderSettings is a configured identity provider.
type ProviderSettings struct {
	Protocol        iam.IdentityProtocol
	Name            string
	Config          iam.IdentityProviderConfig
	JITProvisioning bool
}

// ErrNotConfigured means a self-hosted installation has no Organization yet
// and its configuration does not say how to create one.
var ErrNotConfigured = errors.New("bootstrap: no organization exists; set bootstrap.organization_name and bootstrap.admin_email")

// lockKey serializes bootstraps across replicas.
const lockKey = "iam.bootstrap"

// adminDisplayName names the administrator's account until they rename it.
const adminDisplayName = "Administrator"

// Option configures New.
type Option func(*bootstrapper)

func WithSettings(s Settings) Option { return func(b *bootstrapper) { b.settings = s } }
func WithLogger(l logging.Logger) Option {
	return func(b *bootstrapper) { b.logger = l }
}
func WithOnboarding(o iam.Onboarding) Option { return func(b *bootstrapper) { b.onboarding = o } }
func WithOrganizationRepo(r iam.OrganizationRepository) Option {
	return func(b *bootstrapper) { b.orgs = r }
}
func WithIdentityProviderRepo(r iam.IdentityProviderRepository) Option {
	return func(b *bootstrapper) { b.providers = r }
}
func WithAccountRepo(r iam.AccountRepository) Option { return func(b *bootstrapper) { b.accounts = r } }
func WithPasswordCredentialRepo(r iam.PasswordCredentialRepository) Option {
	return func(b *bootstrapper) { b.passwords = r }
}
func WithUserRepo(r iam.UserRepository) Option   { return func(b *bootstrapper) { b.users = r } }
func WithGrantRepo(r iam.GrantRepository) Option { return func(b *bootstrapper) { b.grants = r } }
func WithOAuthClientRepo(r iam.OAuthClientRepository) Option {
	return func(b *bootstrapper) { b.clients = r }
}
func WithWorkloadRepo(r iam.WorkloadRepository) Option {
	return func(b *bootstrapper) { b.workloads = r }
}
func WithWorkloadCredentialRepo(r iam.WorkloadCredentialRepository) Option {
	return func(b *bootstrapper) { b.credentials = r }
}
func WithPasswordHasher(h hasher.PasswordHasher) Option {
	return func(b *bootstrapper) { b.hasher = h }
}
func WithTransactor(tx transaction.Transactor) Option {
	return func(b *bootstrapper) { b.tx = tx }
}
func WithLocker(l transaction.Locker) Option      { return func(b *bootstrapper) { b.locker = l } }
func WithOutboxRecorder(o outbox.Recorder) Option { return func(b *bootstrapper) { b.outbox = o } }

type bootstrapper struct {
	settings    Settings
	logger      logging.Logger
	onboarding  iam.Onboarding
	orgs        iam.OrganizationRepository
	providers   iam.IdentityProviderRepository
	accounts    iam.AccountRepository
	passwords   iam.PasswordCredentialRepository
	users       iam.UserRepository
	grants      iam.GrantRepository
	clients     iam.OAuthClientRepository
	workloads   iam.WorkloadRepository
	credentials iam.WorkloadCredentialRepository
	hasher      hasher.PasswordHasher
	tx          transaction.Transactor
	locker      transaction.Locker
	outbox      outbox.Recorder
}

// New returns a Bootstrapper, or an error naming a missing dependency.
func New(opts ...Option) (Bootstrapper, error) {
	b := &bootstrapper{}
	for _, o := range opts {
		o(b)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Logger", b.logger != nil},
		{"Onboarding", b.onboarding != nil},
		{"OrganizationRepo", b.orgs != nil},
		{"IdentityProviderRepo", b.providers != nil},
		{"AccountRepo", b.accounts != nil},
		{"PasswordCredentialRepo", b.passwords != nil},
		{"UserRepo", b.users != nil},
		{"GrantRepo", b.grants != nil},
		{"OAuthClientRepo", b.clients != nil},
		{"WorkloadRepo", b.workloads != nil},
		{"WorkloadCredentialRepo", b.credentials != nil},
		{"PasswordHasher", b.hasher != nil},
		{"Transactor", b.tx != nil},
		{"Locker", b.locker != nil},
		{"Outbox", b.outbox != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("bootstrap: %s is required", r.name)
		}
	}
	switch b.settings.Mode {
	case ModeSelfHosted, ModeCloud:
	default:
		return nil, fmt.Errorf("bootstrap: mode %q must be %q or %q", b.settings.Mode, ModeSelfHosted, ModeCloud)
	}
	return b, nil
}

func (b *bootstrapper) Bootstrap(ctx context.Context) (*Installation, error) {
	var inst *Installation
	err := b.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := b.locker.Lock(ctx, lockKey); err != nil {
			return err
		}
		if err := b.applyClients(ctx); err != nil {
			return err
		}
		if err := b.applyPlatformWorkloads(ctx); err != nil {
			return err
		}
		if b.settings.Mode == ModeCloud {
			inst = &Installation{Providers: map[iam.IdentityProtocol]string{}}
			return nil
		}
		org, err := b.installationOrganization(ctx)
		if err != nil {
			return err
		}
		providers, err := b.applyProviders(ctx, org)
		if err != nil {
			return err
		}
		if err := b.seedAdministrator(ctx, org); err != nil {
			return err
		}
		inst = &Installation{OrganizationID: org.ID(), Providers: providers}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inst, nil
}

// applyClients registers the first-party clients, or updates them to match
// the configuration. A client dropped from the configuration stays as it was.
func (b *bootstrapper) applyClients(ctx context.Context) error {
	for _, cs := range b.settings.Clients {
		c, err := b.clients.GetByClientID(ctx, cs.ClientID)
		switch {
		case errors.Is(err, iam.ErrNotFound):
			c, err = iam.NewOAuthClient(iam.NewOAuthClientProps{
				ClientID:     cs.ClientID,
				Registration: iam.RegistrationFirstParty,
				Name:         cs.Name,
				RedirectURIs: cs.RedirectURIs,
			}, nil)
			if err != nil {
				return fmt.Errorf("client %s: %w", cs.ClientID, err)
			}
		case err != nil:
			return err
		default:
			if !c.FirstParty() {
				return fmt.Errorf("bootstrap: client ID %q is taken by a %s client", cs.ClientID, c.Registration())
			}
			if c.Name() == cs.Name && slices.Equal(c.RedirectURIs(), cs.RedirectURIs) {
				continue
			}
			if err := c.Update(cs.Name, cs.RedirectURIs); err != nil {
				return fmt.Errorf("client %s: %w", cs.ClientID, err)
			}
		}
		if err := b.clients.Write(ctx, c); err != nil {
			return fmt.Errorf("write client %s: %w", cs.ClientID, err)
		}
		// Clients are platform-scoped: their events belong to no Organization.
		if err := b.record(ctx, "", iam.AggregateOAuthClient, c.ID(), c.PullEvents()); err != nil {
			return err
		}
		b.logger.Info("registered first-party OAuth client", logging.F("client_id", cs.ClientID))
	}
	return nil
}

// applyPlatformWorkloads makes the platform workloads match the
// configuration, which is their only source: a configured one is created or
// reactivated, its listed keys are added and unlisted ones revoked, and one
// no longer configured is suspended, so its keys stop working.
func (b *bootstrapper) applyPlatformWorkloads(ctx context.Context) error {
	existing, err := b.workloads.ListPlatform(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]*iam.Workload, len(existing))
	for _, w := range existing {
		byName[w.DisplayName()] = w
	}

	for _, ws := range b.settings.PlatformWorkloads {
		w, ok := byName[ws.Name]
		if ok {
			delete(byName, ws.Name)
			if w.Component() != ws.Component {
				return fmt.Errorf("bootstrap: platform workload %q is a %s, not a %s; give the %s another name",
					ws.Name, w.Component(), ws.Component, ws.Component)
			}
			if err := w.Reactivate(); err != nil {
				return fmt.Errorf("platform workload %s: %w", ws.Name, err)
			}
		} else {
			if w, err = iam.NewPlatformWorkload(ws.Name, ws.Component); err != nil {
				return fmt.Errorf("platform workload %s: %w", ws.Name, err)
			}
		}
		if err := b.saveWorkload(ctx, w); err != nil {
			return err
		}
		if err := b.applyPlatformKeys(ctx, w, ws.PublicKeys); err != nil {
			return err
		}
	}
	for _, w := range byName {
		if !w.Active() {
			continue
		}
		if err := w.Suspend(); err != nil {
			return fmt.Errorf("platform workload %s: %w", w.DisplayName(), err)
		}
		if err := b.saveWorkload(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

// applyPlatformKeys adds the listed keys a platform workload lacks and
// revokes its active keys that are no longer listed.
func (b *bootstrapper) applyPlatformKeys(ctx context.Context, w *iam.Workload, keys [][]byte) error {
	stored, err := b.credentials.ListPlatformByPrincipal(ctx, w.ID())
	if err != nil {
		return err
	}
	unlisted := map[string]*iam.WorkloadCredential{}
	for _, c := range stored {
		if c.Kind() == iam.CredentialPublicKey && !c.Revoked() {
			unlisted[c.Identifier()] = c
		}
	}
	for _, der := range keys {
		c, err := iam.NewPlatformKeyCredential(w.ID(), der)
		if err != nil {
			return fmt.Errorf("platform workload %s: key: %w", w.DisplayName(), err)
		}
		if _, ok := unlisted[c.Identifier()]; ok {
			delete(unlisted, c.Identifier())
			continue
		}
		if err := b.saveCredential(ctx, c); err != nil {
			return err
		}
		b.logger.Info("added platform workload key",
			logging.F("workload", w.DisplayName()), logging.F("key_id", c.Identifier()))
	}
	for _, c := range unlisted {
		c.Revoke()
		if err := b.saveCredential(ctx, c); err != nil {
			return err
		}
		b.logger.Info("revoked platform workload key",
			logging.F("workload", w.DisplayName()), logging.F("key_id", c.Identifier()))
	}
	return nil
}

// saveWorkload writes a platform workload only when it changed. Platform
// workloads belong to no Organization, so neither do their events.
func (b *bootstrapper) saveWorkload(ctx context.Context, w *iam.Workload) error {
	raised := w.PullEvents()
	if len(raised) == 0 {
		return nil
	}
	if err := b.workloads.Write(ctx, w); err != nil {
		return fmt.Errorf("write platform workload %s: %w", w.DisplayName(), err)
	}
	b.logger.Info("applied platform workload",
		logging.F("workload", w.DisplayName()), logging.F("status", string(w.Status())))
	return b.record(ctx, "", iam.AggregateWorkload, w.ID(), raised)
}

func (b *bootstrapper) saveCredential(ctx context.Context, c *iam.WorkloadCredential) error {
	if err := b.credentials.Write(ctx, c); err != nil {
		return fmt.Errorf("write platform workload key: %w", err)
	}
	return b.record(ctx, "", iam.AggregateWorkloadCredential, c.ID(), c.PullEvents())
}

// installationOrganization returns the installation's one Organization,
// creating it the first time.
func (b *bootstrapper) installationOrganization(ctx context.Context) (*iam.Organization, error) {
	n, err := b.orgs.Count(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case n == 1:
		return b.orgs.Oldest(ctx)
	case n > 1:
		return nil, fmt.Errorf("bootstrap: a self-hosted installation has one organization, found %d", n)
	}
	s := b.settings
	if s.Organization.Name == "" || s.AdminEmail == "" {
		return nil, ErrNotConfigured
	}
	org, err := b.onboarding.ProvisionInstallation(ctx, iam.ProvisionInstallationInput{
		Slug:        s.Organization.Slug,
		Name:        s.Organization.Name,
		DisplayName: s.Organization.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	b.logger.Info("created the installation's organization", logging.F("organization_id", org.ID()), logging.F("slug", org.Slug()))
	return org, nil
}

// applyProviders makes the Organization's managed identity providers match
// the configuration: configured ones are created or updated and enabled,
// and managed ones no longer configured are disabled. Providers created
// through the API are left alone.
func (b *bootstrapper) applyProviders(ctx context.Context, org *iam.Organization) (map[iam.IdentityProtocol]string, error) {
	existing, err := b.providers.List(ctx, org.ID())
	if err != nil {
		return nil, err
	}
	managed := map[iam.IdentityProtocol]*iam.IdentityProvider{}
	for _, p := range existing {
		if p.ManagedByConfiguration() {
			managed[p.Protocol()] = p
		}
	}

	active := map[iam.IdentityProtocol]string{}
	for _, ps := range b.settings.Providers {
		p, ok := managed[ps.Protocol]
		if ok {
			name, config, jit := ps.Name, ps.Config, ps.JITProvisioning
			if err := p.Configure(iam.UpdateIdentityProviderProps{Name: &name, Config: &config, JITProvisioning: &jit}); err != nil {
				return nil, fmt.Errorf("configure %s provider: %w", ps.Protocol, err)
			}
			delete(managed, ps.Protocol)
		} else {
			p, err = iam.NewIdentityProvider(iam.NewIdentityProviderProps{
				OrganizationID:         org.ID(),
				Protocol:               ps.Protocol,
				Name:                   ps.Name,
				Config:                 ps.Config,
				ManagedByConfiguration: true,
				JITProvisioning:        ps.JITProvisioning,
			}, nil)
			if err != nil {
				return nil, fmt.Errorf("%s provider: %w", ps.Protocol, err)
			}
		}
		if err := b.saveProvider(ctx, org, p); err != nil {
			return nil, err
		}
		active[ps.Protocol] = p.ID()
	}
	for _, p := range managed {
		if err := p.Unconfigure(); err != nil {
			return nil, err
		}
		if err := b.saveProvider(ctx, org, p); err != nil {
			return nil, err
		}
	}
	return active, nil
}

// saveProvider writes a provider only when it changed, so starting again
// with the same configuration writes nothing and records no events.
func (b *bootstrapper) saveProvider(ctx context.Context, org *iam.Organization, p *iam.IdentityProvider) error {
	raised := p.PullEvents()
	if len(raised) == 0 {
		return nil
	}
	if err := b.providers.Write(ctx, p); err != nil {
		return fmt.Errorf("write %s provider: %w", p.Protocol(), err)
	}
	b.logger.Info("applied identity provider", logging.F("protocol", string(p.Protocol())), logging.F("active", p.Active()))
	return b.record(ctx, org.ID(), iam.AggregateIdentityProvider, p.ID(), raised)
}

// seedAdministrator creates the administrator's account with the configured
// password, its membership, and its owner grant, so they can sign in before
// any provider is set up. It does so once: never when the account already
// exists, whose password it would replace, nor once the Organization has an
// owner.
func (b *bootstrapper) seedAdministrator(ctx context.Context, org *iam.Organization) error {
	s := b.settings
	if s.AdminPassword == "" {
		return nil
	}
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	hasOwner, err := b.grants.ExistsForRole(ctx, org.ID(), ownerRole, org.ID())
	if err != nil {
		return err
	}
	if hasOwner {
		return nil
	}
	exists, err := b.accounts.ExistsByEmail(ctx, s.AdminEmail)
	if err != nil {
		return err
	}
	if exists {
		b.logger.Warn("not seeding the administrator's password: an account with the admin email exists", logging.F("email", s.AdminEmail))
		return nil
	}

	account, err := iam.NewAccount(iam.NewAccountProps{Email: s.AdminEmail, DisplayName: adminDisplayName}, nil)
	if err != nil {
		return fmt.Errorf("administrator account: %w", err)
	}
	// The operator chose this address; there is no one to send a check to.
	account.VerifyEmail()
	hash, err := b.hasher.Hash(s.AdminPassword)
	if err != nil {
		return err
	}
	cred, err := iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: account.ID(), PasswordHash: hash})
	if err != nil {
		return err
	}
	user, err := iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: adminDisplayName},
		AccountID:      account.ID(),
	}, nil)
	if err != nil {
		return err
	}
	grant, err := iam.NewOwnerGrant(org.ID(), user.ID())
	if err != nil {
		return err
	}

	if err := b.accounts.Write(ctx, account); err != nil {
		return fmt.Errorf("write administrator account: %w", err)
	}
	if err := b.passwords.Write(ctx, cred); err != nil {
		return fmt.Errorf("write administrator password: %w", err)
	}
	if err := b.users.Write(ctx, user); err != nil {
		return fmt.Errorf("write administrator membership: %w", err)
	}
	if err := b.grants.Write(ctx, grant); err != nil {
		return fmt.Errorf("write administrator grant: %w", err)
	}
	for _, src := range []struct {
		typ string
		id  string
		obj interface{ PullEvents() []aggregate.Event }
	}{
		{iam.AggregateAccount, account.ID(), account},
		{iam.AggregatePasswordCredential, account.ID(), cred},
		{iam.AggregateUser, user.ID(), user},
		{iam.AggregateGrant, grant.ID(), grant},
	} {
		if err := b.record(ctx, org.ID(), src.typ, src.id, src.obj.PullEvents()); err != nil {
			return err
		}
	}
	b.logger.Info("created the administrator's account", logging.F("email", strings.ToLower(s.AdminEmail)))
	return nil
}

func (b *bootstrapper) record(ctx context.Context, organizationID, aggregateType, aggregateID string, raised []aggregate.Event) error {
	envelopes, err := events.Wrap(ctx, events.Source{OrganizationID: organizationID, AggregateType: aggregateType, AggregateID: aggregateID}, raised...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", aggregateType, err)
	}
	if err := b.outbox.Record(ctx, envelopes...); err != nil {
		return fmt.Errorf("record %s events: %w", aggregateType, err)
	}
	return nil
}
