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

type OrganizationRepository interface {
	ExistsByName(ctx context.Context, name string) (bool, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	GetByIdentifier(ctx context.Context, identifier string) (*Organization, error)
	GetByIdentifiers(ctx context.Context, identifiers []string) ([]*Organization, error)
	Write(ctx context.Context, org *Organization) error
}

type AccountRepository interface {
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Write(ctx context.Context, account *Account) error
}

type PasswordCredentialRepository interface {
	Write(ctx context.Context, cred *PasswordCredential) error
}

type UserRepository interface {
	Write(ctx context.Context, user *User) error
}
