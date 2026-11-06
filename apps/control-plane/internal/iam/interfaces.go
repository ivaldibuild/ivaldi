package iam

import (
	"context"
)

// ResourceRegistrar keeps IAM's copy of the resource hierarchy in step with
// the capabilities that own the entities, such as Workspaces, Projects,
// Sources, and Environments. Calls run in the caller's transaction, so an
// entity never exists without its resource.
type ResourceRegistrar interface {
	RegisterResource(ctx context.Context, r Resource) error
	// ArchiveResource soft-deletes the resource, so grants on it stop applying.
	ArchiveResource(ctx context.Context, organizationID, resourceID string) error
	RestoreResource(ctx context.Context, organizationID, resourceID string) error
}

// Onboarding creates Organizations. Each gets its place in the resource
// hierarchy and, once someone holds it, an owner.
type Onboarding interface {
	OnboardOrganization(ctx context.Context, input *OnboardOrganizationInput) error
	// ProvisionInstallation creates a self-hosted installation's
	// Organization, with no owner yet: the configured administrator claims
	// it on first sign-in.
	ProvisionInstallation(ctx context.Context, input ProvisionInstallationInput) (*Organization, error)
	// OnboardWithPlatformIdentity signs a new person up for the managed
	// cloud with a platform provider's verified identity: their Account,
	// its link to the provider, and a new Organization they own. It returns
	// the Account's ID.
	OnboardWithPlatformIdentity(ctx context.Context, input OnboardWithPlatformIdentityInput) (string, error)
}

// Authentication signs people in. It decides who someone is and which
// Organizations they may act in; the authorization server then issues
// tokens for the one they choose.
type Authentication interface {
	// AuthenticatePassword checks a built-in account's email and password.
	// A wrong email and a wrong password are the same ErrInvalidCredentials.
	AuthenticatePassword(ctx context.Context, email, password string) (*SignedIn, error)
	// AuthenticatePlatform signs a person in to the managed cloud with a
	// platform provider. A subject it has not seen links to an Account only
	// when both the Account's and the provider's emails are verified;
	// otherwise the person must sign up (ErrSignUpRequired) or sign in
	// another way first (ErrAccountExists).
	AuthenticatePlatform(ctx context.Context, provider PlatformProvider, profile ExternalProfile) (*SignedIn, error)
	// AuthenticateExternal signs a person in through one of an
	// Organization's identity providers, linking or provisioning their
	// Account and User as the provider allows. The sign-in is restricted to
	// that Organization.
	AuthenticateExternal(ctx context.Context, organizationID, identityProviderID string, profile ExternalProfile) (*SignedIn, error)
	// Memberships returns the Account's active memberships, oldest first,
	// for choosing an Organization.
	Memberships(ctx context.Context, accountID string) ([]Membership, error)
	// Membership returns the Account's membership in the Organization if
	// it may act there now, or ErrNoMembership. Callers check it again
	// before issuing anything, since a membership can end after sign-in.
	Membership(ctx context.Context, accountID, organizationID string) (*Membership, error)
}

// Repositories persist and retrieve; they decide nothing. Write inserts an
// object, or updates it when its key is stored. A unique violation is
// ErrAlreadyExists, and a reference to a missing record ErrNotFound. Every
// repository joins the transaction its context carries.

type OrganizationRepository interface {
	ExistsByName(ctx context.Context, name string) (bool, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	GetByIdentifier(ctx context.Context, identifier string) (*Organization, error)
	GetByIdentifiers(ctx context.Context, identifiers []string) ([]*Organization, error)
	// Count returns how many Organizations exist, whatever their status.
	Count(ctx context.Context) (int64, error)
	// Oldest returns the first Organization created, such as a self-hosted
	// installation's only one.
	Oldest(ctx context.Context) (*Organization, error)
	// GetForUpdate reads the Organization and locks its row until the
	// transaction ends, so checks made on it, such as whether it has an
	// owner yet, hold until the change commits.
	GetForUpdate(ctx context.Context, organizationID string) (*Organization, error)
	Write(ctx context.Context, org *Organization) error
}

type AccountRepository interface {
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Get(ctx context.Context, accountID string) (*Account, error)
	// GetByEmail matches the email ignoring case, as its unique index does.
	GetByEmail(ctx context.Context, email string) (*Account, error)
	Write(ctx context.Context, account *Account) error
}

type PasswordCredentialRepository interface {
	Get(ctx context.Context, accountID string) (*PasswordCredential, error)
	Write(ctx context.Context, cred *PasswordCredential) error
}

type UserRepository interface {
	Get(ctx context.Context, organizationID, principalID string) (*User, error)
	// GetByAccount returns the Account's User in the Organization.
	GetByAccount(ctx context.Context, organizationID, accountID string) (*User, error)
	// ListMemberships returns the Account's Users in every Organization,
	// oldest first. It is the one read across Organizations, for choosing
	// one at sign-in.
	ListMemberships(ctx context.Context, accountID string) ([]Membership, error)
	Write(ctx context.Context, user *User) error
}

type IdentityProviderRepository interface {
	Get(ctx context.Context, organizationID, identityProviderID string) (*IdentityProvider, error)
	// List returns the Organization's identity providers, oldest first.
	List(ctx context.Context, organizationID string) ([]*IdentityProvider, error)
	Write(ctx context.Context, p *IdentityProvider) error
}

type ExternalIdentityRepository interface {
	GetBySubject(ctx context.Context, organizationID, identityProviderID, subject string) (*ExternalIdentity, error)
	Write(ctx context.Context, e *ExternalIdentity) error
}

type AccountIdentityRepository interface {
	GetBySubject(ctx context.Context, provider PlatformProvider, subject string) (*AccountIdentity, error)
	Write(ctx context.Context, a *AccountIdentity) error
}

// OrganizationProvisioner sets up what every new Organization starts with in
// other capabilities, such as its default Workspace. IAM calls it inside the
// transaction that creates the Organization, so if it fails the Organization
// is not created. The control plane wires it at startup, so IAM depends on no
// other capability.
type OrganizationProvisioner interface {
	ProvisionOrganization(ctx context.Context, organizationID string) error
}

// AccessRepository reads what an authorization check needs.
type AccessRepository interface {
	Access(ctx context.Context, q AccessQuery) (Access, error)
}

type GrantRepository interface {
	// ExistsForRole reports whether anyone holds the role on the resource,
	// such as whether an Organization has an owner.
	ExistsForRole(ctx context.Context, organizationID, roleID, resourceID string) (bool, error)
	// Write inserts the Grant; grants never change.
	Write(ctx context.Context, g *Grant) error
}

type OAuthClientRepository interface {
	Get(ctx context.Context, id string) (*OAuthClient, error)
	// GetByClientID finds a client by its public client_id.
	GetByClientID(ctx context.Context, clientID string) (*OAuthClient, error)
	Write(ctx context.Context, c *OAuthClient) error
}

type OAuthAuthorizationCodeRepository interface {
	// GetForUpdate finds a code by its hash and locks it, so it is redeemed
	// at most once even when two exchanges race.
	GetForUpdate(ctx context.Context, codeHash []byte) (*OAuthAuthorizationCode, error)
	Write(ctx context.Context, c *OAuthAuthorizationCode) error
}

type OAuthRefreshTokenRepository interface {
	// GetByHashForUpdate finds a token by its hash and locks it, so it is
	// rotated at most once even when two refreshes race.
	GetByHashForUpdate(ctx context.Context, tokenHash []byte) (*OAuthRefreshToken, error)
	// ListActiveByFamily returns the family's tokens that are not revoked,
	// to revoke them all when a rotated token is replayed.
	ListActiveByFamily(ctx context.Context, organizationID, familyID string) ([]*OAuthRefreshToken, error)
	// Write saves the token. A rotated token refers to its replacement, so
	// write the replacement first.
	Write(ctx context.Context, t *OAuthRefreshToken) error
}
