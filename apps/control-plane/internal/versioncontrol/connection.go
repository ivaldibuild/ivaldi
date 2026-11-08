package versioncontrol

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// AuthKind is how Ivaldi authenticates to a provider.
type AuthKind string

const (
	AuthKindAppInstallation AuthKind = "app_installation"
	AuthKindOAuth           AuthKind = "oauth"
	AuthKindAccessToken     AuthKind = "access_token"
)

// Status is a Connection's standing with its provider.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusRevoked   Status = "revoked"
)

// Connection is how an Organization reaches one provider account, such as a
// GitHub organization with Ivaldi's App installed. It stores references to
// credentials, never secrets.
type Connection struct {
	aggregate.Root
	organizationID      string
	provider            vcs.ProviderKind
	hostURL             string
	authKind            AuthKind
	credentialRef       *string
	externalAccountID   string
	externalAccountName *string
	webhookRouteKey     *string
	lookupFrom          vcs.LookupFrom
	lookupPool          *string
	status              Status
	createdBy           *string
	createdAt           time.Time
	updatedAt           time.Time
	deletedAt           *time.Time
}

// ConnectionSnapshot is a Connection's state, for repositories, audit
// records, and events. Its JSON form is part of the Connection events'
// contract.
type ConnectionSnapshot struct {
	ID                   string           `json:"id"`
	OrganizationID       string           `json:"organization_id"`
	Provider             vcs.ProviderKind `json:"provider" enum:"github,gitlab,bitbucket,gitea,azure_devops"`
	HostURL              string           `json:"host_url"`
	AuthKind             AuthKind         `json:"auth_kind" enum:"app_installation,oauth,access_token"`
	CredentialRef        *string          `json:"credential_ref"`
	ExternalAccountID    string           `json:"external_account_id"`
	ExternalAccountName  *string          `json:"external_account_name"`
	WebhookRouteKey      *string          `json:"webhook_route_key"`
	LookupFrom           vcs.LookupFrom   `json:"lookup_from" enum:"control_plane,data_plane"`
	LookupPool           *string          `json:"lookup_pool"`
	Status               Status           `json:"status" enum:"active,suspended,revoked"`
	CreatedByPrincipalID *string          `json:"created_by_principal_id"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	DeletedAt            *time.Time       `json:"deleted_at"`
}

// NewConnectionProps describes a Connection. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewConnectionProps struct {
	OrganizationID      string
	Provider            vcs.ProviderKind
	HostURL             string
	AuthKind            AuthKind
	CredentialRef       *string
	ExternalAccountID   string
	ExternalAccountName *string
	// WebhookRouteKey identifies the provider account across the whole
	// platform, so one account belongs to one Organization.
	WebhookRouteKey      *string
	LookupFrom           vcs.LookupFrom
	LookupPool           *string
	Status               Status
	CreatedByPrincipalID *string
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	DeletedAt            *time.Time
}

// NewConnection returns a Connection, with a new ID unless connectionID
// gives a stored one.
func NewConnection(props NewConnectionProps, connectionID *string) (*Connection, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !props.Provider.Valid() {
		return nil, NewErrInvalidInput(fmt.Sprintf("provider %q is not supported", props.Provider))
	}
	if err := validateText("host URL", props.HostURL, maxHostURLLength, true); err != nil {
		return nil, err
	}
	if _, err := parseURL(props.HostURL); err != nil {
		return nil, err
	}
	if err := validateAuthKind(props.AuthKind); err != nil {
		return nil, err
	}
	if err := validateText("external account ID", props.ExternalAccountID, maxExternalIDLength, true); err != nil {
		return nil, err
	}
	if err := validateText("external account name", deref(props.ExternalAccountName), maxExternalNameLength, false); err != nil {
		return nil, err
	}
	if err := validateText("webhook route key", deref(props.WebhookRouteKey), maxWebhookRouteKeyLength, false); err != nil {
		return nil, err
	}
	if err := validateLookup(props.LookupFrom, props.LookupPool); err != nil {
		return nil, err
	}
	if err := validateStatus(props.Status); err != nil {
		return nil, err
	}
	root, err := newRoot(Connection{}.Prefix(), connectionID)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	c := &Connection{
		Root:                root,
		organizationID:      props.OrganizationID,
		provider:            props.Provider,
		hostURL:             props.HostURL,
		authKind:            props.AuthKind,
		credentialRef:       optional.OfPtr(props.CredentialRef),
		externalAccountID:   props.ExternalAccountID,
		externalAccountName: optional.OfPtr(props.ExternalAccountName),
		webhookRouteKey:     optional.OfPtr(props.WebhookRouteKey),
		lookupFrom:          props.LookupFrom,
		lookupPool:          optional.OfPtr(props.LookupPool),
		status:              props.Status,
		createdBy:           optional.OfPtr(props.CreatedByPrincipalID),
		createdAt:           timeOr(props.CreatedAt, now),
		updatedAt:           timeOr(props.UpdatedAt, now),
		deletedAt:           clock.CopyTime(props.DeletedAt),
	}
	if c.IsNew() {
		c.RecordEvent(ConnectionCreated{c.Snapshot()})
	}
	return c, nil
}

// NewInstalledConnection returns a Connection to a provider account the
// provider confirmed the creator can access, through an install flow. Its
// webhook route key is unique across the platform, so the account cannot be
// connected to a second Organization.
func NewInstalledConnection(organizationID string, createdByPrincipalID *string, in vcs.Installation) (*Connection, error) {
	routeKey := providerKey(in.Provider, in.HostURL, in.InstallationID)
	return NewConnection(NewConnectionProps{
		OrganizationID:       organizationID,
		Provider:             in.Provider,
		HostURL:              in.HostURL,
		AuthKind:             AuthKindAppInstallation,
		ExternalAccountID:    in.InstallationID,
		ExternalAccountName:  optional.Of(in.AccountName),
		WebhookRouteKey:      &routeKey,
		LookupFrom:           vcs.LookupFromControlPlane,
		Status:               StatusActive,
		CreatedByPrincipalID: createdByPrincipalID,
	}, nil)
}

func validateAuthKind(k AuthKind) error {
	switch k {
	case AuthKindAppInstallation, AuthKindOAuth, AuthKindAccessToken:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("auth kind %q must be app_installation, oauth, or access_token", k))
}

func validateStatus(s Status) error {
	switch s {
	case StatusActive, StatusSuspended, StatusRevoked:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("status %q must be active, suspended, or revoked", s))
}

func validateLookup(from vcs.LookupFrom, pool *string) error {
	switch from {
	case vcs.LookupFromControlPlane:
		return nil
	case vcs.LookupFromDataPlane:
		if deref(pool) == "" {
			return NewErrInvalidInput("a data-plane lookup needs a lookup pool")
		}
		return validateText("lookup pool", deref(pool), maxLookupPoolLength, true)
	}
	return NewErrInvalidInput(fmt.Sprintf("lookup_from %q must be control_plane or data_plane", from))
}

func (Connection) Prefix() string                   { return "vcn" }
func (c *Connection) OrganizationID() string        { return c.organizationID }
func (c *Connection) Provider() vcs.ProviderKind    { return c.provider }
func (c *Connection) HostURL() string               { return c.hostURL }
func (c *Connection) AuthKind() AuthKind            { return c.authKind }
func (c *Connection) CredentialRef() *string        { return copyString(c.credentialRef) }
func (c *Connection) ExternalAccountID() string     { return c.externalAccountID }
func (c *Connection) ExternalAccountName() *string  { return copyString(c.externalAccountName) }
func (c *Connection) WebhookRouteKey() *string      { return copyString(c.webhookRouteKey) }
func (c *Connection) LookupFrom() vcs.LookupFrom    { return c.lookupFrom }
func (c *Connection) LookupPool() *string           { return copyString(c.lookupPool) }
func (c *Connection) Status() Status                { return c.status }
func (c *Connection) CreatedByPrincipalID() *string { return copyString(c.createdBy) }
func (c *Connection) CreatedAt() time.Time          { return c.createdAt }
func (c *Connection) UpdatedAt() time.Time          { return c.updatedAt }
func (c *Connection) DeletedAt() *time.Time         { return clock.CopyTime(c.deletedAt) }

// Archived reports whether the Connection was removed.
func (c *Connection) Archived() bool { return c.deletedAt != nil }

// Usable reports whether the Connection may be used for lookups.
func (c *Connection) Usable() bool { return !c.Archived() && c.status == StatusActive }

// CheckControlPlaneLookup refuses a lookup from the control plane through a
// Connection that is not usable, or whose lookups run in the data plane
// (ADR 0008).
func (c *Connection) CheckControlPlaneLookup() error {
	if !c.Usable() {
		return fmt.Errorf("%w: connection %s", ErrInactiveConnection, c.ID())
	}
	if c.lookupFrom == vcs.LookupFromDataPlane {
		return ErrLookupInDataPlane
	}
	return nil
}

// RepositoryKey is the key a Source connecting the account's repository
// would have, unique among the Organization's active Sources.
func (c *Connection) RepositoryKey(externalRepositoryID string) string {
	return providerKey(c.provider, c.hostURL, externalRepositoryID)
}

// Endpoint returns how a provider reaches this account.
func (c *Connection) Endpoint() vcs.Endpoint {
	return vcs.Endpoint{
		Provider:       c.provider,
		HostURL:        c.hostURL,
		InstallationID: c.externalAccountID,
		CredentialRef:  copyString(c.credentialRef),
	}
}

func (c *Connection) Snapshot() ConnectionSnapshot {
	return ConnectionSnapshot{
		ID:                   c.ID(),
		OrganizationID:       c.organizationID,
		Provider:             c.provider,
		HostURL:              c.hostURL,
		AuthKind:             c.authKind,
		CredentialRef:        copyString(c.credentialRef),
		ExternalAccountID:    c.externalAccountID,
		ExternalAccountName:  copyString(c.externalAccountName),
		WebhookRouteKey:      copyString(c.webhookRouteKey),
		LookupFrom:           c.lookupFrom,
		LookupPool:           copyString(c.lookupPool),
		Status:               c.status,
		CreatedByPrincipalID: copyString(c.createdBy),
		CreatedAt:            c.createdAt,
		UpdatedAt:            c.updatedAt,
		DeletedAt:            clock.CopyTime(c.deletedAt),
	}
}

// Archive soft-deletes the Connection. Its Sources remain, but lookups
// through it stop.
func (c *Connection) Archive() {
	if c.Archived() {
		return
	}
	now := clock.Now()
	c.deletedAt = &now
	c.updatedAt = now
	c.RecordEvent(ConnectionArchived{c.Snapshot()})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
