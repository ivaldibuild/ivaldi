package iam

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// IdentityProtocol is how an identity provider signs people in.
type IdentityProtocol string

const (
	ProtocolOIDC IdentityProtocol = "oidc"
	ProtocolSAML IdentityProtocol = "saml"
	// ProtocolBuiltin signs people in with a PasswordCredential on their Account.
	ProtocolBuiltin IdentityProtocol = "builtin"
	// ProtocolGoogle is OIDC with Google's fixed issuer.
	ProtocolGoogle IdentityProtocol = "google"
	// ProtocolGitHub is OAuth 2.0 plus GitHub's user API, because GitHub
	// issues no ID token. A GitHub identity's subject is its numeric user ID.
	ProtocolGitHub IdentityProtocol = "github"
)

// IdentityProviderStatus says whether an identity provider accepts sign-ins.
type IdentityProviderStatus string

const (
	IdentityProviderActive   IdentityProviderStatus = "active"
	IdentityProviderDisabled IdentityProviderStatus = "disabled"
)

// IdentityProviderConfig holds an identity provider's non-secret settings.
// Which fields apply depends on the protocol. It is a value object.
type IdentityProviderConfig struct {
	// Issuer is the OIDC issuer URL, required for oidc.
	Issuer string `json:"issuer"`
	// ClientID is required for oidc, google, and github.
	ClientID string `json:"client_id"`
	// MetadataURL is the SAML metadata URL, required for saml.
	MetadataURL string `json:"metadata_url"`
	// BaseURL is a GitHub Enterprise Server address, for github only.
	BaseURL string `json:"base_url"`
	// AllowedDomains, AllowedOrganizations, and AllowedEmails say who may
	// sign in through google or github, which anyone can otherwise use; at
	// least one is required. Domains are Google Workspace domains, for google
	// only; organizations are GitHub organizations, for github only.
	AllowedDomains       []string `json:"allowed_domains"`
	AllowedOrganizations []string `json:"allowed_organizations"`
	AllowedEmails        []string `json:"allowed_emails"`
}

func (c IdentityProviderConfig) clone() IdentityProviderConfig {
	c.AllowedDomains = copyStrings(c.AllowedDomains)
	c.AllowedOrganizations = copyStrings(c.AllowedOrganizations)
	c.AllowedEmails = copyStrings(c.AllowedEmails)
	return c
}

func (c IdentityProviderConfig) equal(o IdentityProviderConfig) bool {
	return c.Issuer == o.Issuer && c.ClientID == o.ClientID && c.MetadataURL == o.MetadataURL && c.BaseURL == o.BaseURL &&
		slices.Equal(c.AllowedDomains, o.AllowedDomains) && slices.Equal(c.AllowedOrganizations, o.AllowedOrganizations) &&
		slices.Equal(c.AllowedEmails, o.AllowedEmails)
}

// IdentityProvider is how people sign in to one Organization. Its secrets,
// such as an OAuth client secret, are held by the credentials capability and
// referred to by SecretRef (ADR 0004).
//
// In a self-hosted installation, providers set in the control plane's
// configuration are applied on every start and are managed by it: only
// Configure and Unconfigure change them.
type IdentityProvider struct {
	aggregate.Root
	organizationID         string
	protocol               IdentityProtocol
	name                   string
	config                 IdentityProviderConfig
	managedByConfiguration bool
	secretRef              *string
	jitProvisioning        bool
	status                 IdentityProviderStatus
	createdAt              time.Time
	updatedAt              time.Time
}

// IdentityProviderSnapshot is an IdentityProvider's state, for repositories,
// audit records, and events. Its JSON form is part of the IdentityProvider
// events' contract. ManagedByConfiguration is stored in the config column as
// managed_by.
type IdentityProviderSnapshot struct {
	ID                     string                 `json:"id"`
	OrganizationID         string                 `json:"organization_id"`
	Protocol               IdentityProtocol       `json:"protocol"`
	Name                   string                 `json:"name"`
	Config                 IdentityProviderConfig `json:"config"`
	ManagedByConfiguration bool                   `json:"managed_by_configuration"`
	SecretRef              *string                `json:"secret_ref"`
	JITProvisioning        bool                   `json:"jit_provisioning"`
	Status                 IdentityProviderStatus `json:"status"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

// NewIdentityProviderProps describes an IdentityProvider. A new one leaves
// Status and the timestamps empty; a repository loading a stored one sets them.
type NewIdentityProviderProps struct {
	OrganizationID         string
	Protocol               IdentityProtocol
	Name                   string
	Config                 IdentityProviderConfig
	ManagedByConfiguration bool
	SecretRef              *string
	// JITProvisioning lets a first sign-in create the User.
	JITProvisioning bool
	// Status defaults to IdentityProviderActive.
	Status    IdentityProviderStatus
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// UpdateIdentityProviderProps changes the fields that are set. An empty
// SecretRef removes it.
type UpdateIdentityProviderProps struct {
	Name            *string
	Config          *IdentityProviderConfig
	SecretRef       *string
	JITProvisioning *bool
}

// NewIdentityProvider returns an IdentityProvider, with a new ID unless
// identityProviderID gives a stored one.
func NewIdentityProvider(props NewIdentityProviderProps, identityProviderID *string) (*IdentityProvider, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(IdentityProvider{}.Prefix(), identityProviderID)
	if err != nil {
		return nil, err
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}
	if err := validateProviderConfig(props.Protocol, props.Config); err != nil {
		return nil, err
	}
	secretRef := optionalString(props.SecretRef)
	if props.Protocol == ProtocolBuiltin && secretRef != nil {
		return nil, NewErrInvalidInput("the builtin identity provider has no secret")
	}
	if props.Status == "" {
		props.Status = IdentityProviderActive
	}
	if err := validateIdentityProviderStatus(props.Status); err != nil {
		return nil, err
	}

	now := clock.Now()
	p := &IdentityProvider{
		Root:                   root,
		organizationID:         props.OrganizationID,
		protocol:               props.Protocol,
		name:                   props.Name,
		config:                 props.Config.clone(),
		managedByConfiguration: props.ManagedByConfiguration,
		secretRef:              secretRef,
		jitProvisioning:        props.JITProvisioning,
		status:                 props.Status,
		createdAt:              timeOr(props.CreatedAt, now),
		updatedAt:              timeOr(props.UpdatedAt, now),
	}
	if p.IsNew() {
		p.RecordEvent(IdentityProviderCreated{p.Snapshot()})
	}
	return p, nil
}

// validateProviderConfig checks that config has what protocol needs and
// nothing that applies only to another protocol.
func validateProviderConfig(protocol IdentityProtocol, c IdentityProviderConfig) error {
	set := map[string]bool{
		"issuer":                c.Issuer != "",
		"client ID":             c.ClientID != "",
		"metadata URL":          c.MetadataURL != "",
		"base URL":              c.BaseURL != "",
		"allowed domains":       len(c.AllowedDomains) > 0,
		"allowed organizations": len(c.AllowedOrganizations) > 0,
		"allowed emails":        len(c.AllowedEmails) > 0,
	}
	var required, allowed []string
	switch protocol {
	case ProtocolOIDC:
		required = []string{"issuer", "client ID"}
	case ProtocolSAML:
		required = []string{"metadata URL"}
	case ProtocolBuiltin:
	case ProtocolGoogle:
		required = []string{"client ID"}
		allowed = []string{"allowed domains", "allowed emails"}
	case ProtocolGitHub:
		required = []string{"client ID"}
		allowed = []string{"base URL", "allowed organizations", "allowed emails"}
	default:
		return NewErrInvalidInput(fmt.Sprintf("identity provider protocol %q must be oidc, saml, builtin, google, or github", protocol))
	}
	for _, field := range required {
		if !set[field] {
			return NewErrInvalidInput(fmt.Sprintf("a %s identity provider needs a %s", protocol, field))
		}
	}
	for field, ok := range set {
		if ok && !slices.Contains(required, field) && !slices.Contains(allowed, field) {
			return NewErrInvalidInput(fmt.Sprintf("a %s identity provider has no %s", protocol, field))
		}
	}
	if (protocol == ProtocolGoogle || protocol == ProtocolGitHub) &&
		!set["allowed domains"] && !set["allowed organizations"] && !set["allowed emails"] {
		return NewErrInvalidInput(fmt.Sprintf("a %s identity provider must say who may sign in: anyone can use %s", protocol, protocol))
	}

	for _, u := range []struct{ field, value string }{
		{"issuer", c.Issuer}, {"metadata URL", c.MetadataURL}, {"base URL", c.BaseURL},
	} {
		if u.value != "" {
			if err := validateHTTPSURL(u.field, u.value); err != nil {
				return err
			}
		}
	}
	for _, d := range c.AllowedDomains {
		if err := validateDomain(d); err != nil {
			return err
		}
		if d != strings.ToLower(d) {
			return NewErrInvalidInput(fmt.Sprintf("allowed domain %q must be lowercase", d))
		}
	}
	for _, o := range c.AllowedOrganizations {
		if err := validateName("allowed organization", o, maxNameLength); err != nil {
			return err
		}
	}
	for _, e := range c.AllowedEmails {
		if err := validateEmail(e); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentityProviderStatus(s IdentityProviderStatus) error {
	switch s {
	case IdentityProviderActive, IdentityProviderDisabled:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("identity provider status %q must be active or disabled", s))
}

func (IdentityProvider) Prefix() string                    { return "idp" }
func (p *IdentityProvider) OrganizationID() string         { return p.organizationID }
func (p *IdentityProvider) Protocol() IdentityProtocol     { return p.protocol }
func (p *IdentityProvider) Name() string                   { return p.name }
func (p *IdentityProvider) Config() IdentityProviderConfig { return p.config.clone() }
func (p *IdentityProvider) ManagedByConfiguration() bool   { return p.managedByConfiguration }
func (p *IdentityProvider) SecretRef() *string             { return copyString(p.secretRef) }
func (p *IdentityProvider) JITProvisioning() bool          { return p.jitProvisioning }
func (p *IdentityProvider) Status() IdentityProviderStatus { return p.status }
func (p *IdentityProvider) CreatedAt() time.Time           { return p.createdAt }
func (p *IdentityProvider) UpdatedAt() time.Time           { return p.updatedAt }

func (p *IdentityProvider) Active() bool { return p.status == IdentityProviderActive }

func (p *IdentityProvider) Snapshot() IdentityProviderSnapshot {
	return IdentityProviderSnapshot{
		ID:                     p.ID(),
		OrganizationID:         p.organizationID,
		Protocol:               p.protocol,
		Name:                   p.name,
		Config:                 p.config.clone(),
		ManagedByConfiguration: p.managedByConfiguration,
		SecretRef:              copyString(p.secretRef),
		JITProvisioning:        p.jitProvisioning,
		Status:                 p.status,
		CreatedAt:              p.createdAt,
		UpdatedAt:              p.updatedAt,
	}
}

func (p *IdentityProvider) checkNotManaged() error {
	if p.managedByConfiguration {
		return fmt.Errorf("%w: identity provider %s", ErrManagedByConfiguration, p.ID())
	}
	return nil
}

// Update changes a provider that is not managed by configuration. Nothing
// changes if any field is invalid.
func (p *IdentityProvider) Update(props UpdateIdentityProviderProps) error {
	if err := p.checkNotManaged(); err != nil {
		return err
	}
	changed, err := p.update(props)
	if err != nil {
		return err
	}
	if changed {
		p.RecordEvent(IdentityProviderUpdated{p.Snapshot()})
	}
	return nil
}

// update applies props and reports whether anything changed.
func (p *IdentityProvider) update(props UpdateIdentityProviderProps) (bool, error) {
	if props.Name != nil {
		if err := validateName("name", *props.Name, maxNameLength); err != nil {
			return false, err
		}
	}
	if props.Config != nil {
		if err := validateProviderConfig(p.protocol, *props.Config); err != nil {
			return false, err
		}
	}
	secretRef := optionalString(props.SecretRef)
	if p.protocol == ProtocolBuiltin && secretRef != nil {
		return false, NewErrInvalidInput("the builtin identity provider has no secret")
	}

	changed := anyTrue(
		set(&p.name, props.Name),
		setOptional(&p.secretRef, props.SecretRef),
		set(&p.jitProvisioning, props.JITProvisioning),
	)
	if props.Config != nil && !props.Config.equal(p.config) {
		p.config = props.Config.clone()
		changed = true
	}
	p.updatedAt = clock.Now()
	return changed, nil
}

// Enable lets a provider that is not managed by configuration accept
// sign-ins again. Enabling an active one does nothing.
func (p *IdentityProvider) Enable() error {
	if err := p.checkNotManaged(); err != nil {
		return err
	}
	p.setStatus(IdentityProviderActive)
	return nil
}

// Disable stops a provider that is not managed by configuration from
// accepting sign-ins. Its external identities are kept. Disabling a
// disabled one does nothing.
func (p *IdentityProvider) Disable() error {
	if err := p.checkNotManaged(); err != nil {
		return err
	}
	p.setStatus(IdentityProviderDisabled)
	return nil
}

// Configure applies the installation's configuration to the provider:
// the configured fields replace the stored ones, and the provider becomes
// active and managed by configuration. Every field of props is applied.
func (p *IdentityProvider) Configure(props UpdateIdentityProviderProps) error {
	if props.Name == nil || props.Config == nil || props.JITProvisioning == nil {
		return NewErrInvalidInput("configuring an identity provider needs its name, config, and JIT provisioning")
	}
	if props.SecretRef == nil {
		props.SecretRef = new("")
	}
	changed, err := p.update(props)
	if err != nil {
		return err
	}
	if !p.managedByConfiguration || p.status != IdentityProviderActive {
		changed = true
	}
	p.managedByConfiguration = true
	p.status = IdentityProviderActive
	if changed {
		p.RecordEvent(IdentityProviderConfigured{p.Snapshot()})
	}
	return nil
}

// Unconfigure disables a provider that was removed from the installation's
// configuration. It stays managed by configuration, so only adding it back
// to the configuration enables it again.
func (p *IdentityProvider) Unconfigure() error {
	if !p.managedByConfiguration {
		return NewErrInvalidInput(fmt.Sprintf("identity provider %s is not managed by configuration", p.ID()))
	}
	p.setStatus(IdentityProviderDisabled)
	return nil
}

func (p *IdentityProvider) setStatus(s IdentityProviderStatus) {
	if p.status != s {
		previous := p.status
		p.status = s
		p.updatedAt = clock.Now()
		p.RecordEvent(IdentityProviderStatusChanged{IdentityProvider: p.Snapshot(), PreviousStatus: previous})
	}
}
