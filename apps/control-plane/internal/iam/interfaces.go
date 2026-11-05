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

type Onboarding interface {
	OnboardOrganization(ctx context.Context, input *OnboardOrganizationInput) error
}

type Authentication interface{}

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
