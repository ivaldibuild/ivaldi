package iam

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// OrganizationService reads and changes the caller's Organization. Any
// member may read it; owners and admins may change it. A slug change takes
// effect at once: links and sign-in hints that name the old slug stop
// matching, while ones that name the Organization's ID keep working.
type OrganizationService interface {
	GetOrganization(ctx context.Context, actor identity.Actor, organizationID string) (*Organization, error)
	// UpdateOrganization changes the fields props sets. A slug another
	// Organization holds fails with ErrAlreadyExists.
	UpdateOrganization(ctx context.Context, actor identity.Actor, organizationID string, props UpdateOrganizationProps) (*Organization, error)
}

// GrantService grants and removes roles on resources. Only someone allowed
// to manage grants on a resource may change them there, and only owners may
// grant or remove the owner role. Agent Sessions may not change grants
// without a human's approval, which the authorizer refuses for now.
type GrantService interface {
	// ListGrants lists the grants on a resource, and optionally on the
	// resources above it.
	ListGrants(ctx context.Context, actor identity.Actor, query ListResourceGrantsQuery) (*pagination.Result[GrantEntry], error)
	// CreateGrant grants a role where role.go allows it. The same role on
	// the same resource for the same grantee is ErrAlreadyExists.
	CreateGrant(ctx context.Context, actor identity.Actor, input CreateGrantInput) (*GrantEntry, error)
	// RemoveGrant removes a grant. Removing the Organization's last owner is
	// ErrLastOwner.
	RemoveGrant(ctx context.Context, actor identity.Actor, organizationID, grantID string) error
}

// ServiceAccountRepository stores ServiceAccounts: a principal row and a
// service_accounts row each.
type ServiceAccountRepository interface {
	// Write inserts the ServiceAccount, or updates its principal when its
	// ID is stored.
	Write(ctx context.Context, s *ServiceAccount) error
	Get(ctx context.Context, organizationID, principalID string) (*ServiceAccount, error)
	// List returns the Organization's ServiceAccounts by name, the active
	// ones only unless includeInactive.
	List(ctx context.Context, organizationID string, includeInactive bool) ([]*ServiceAccount, error)
}

// APITokenRepository stores API tokens, of which only a hash is kept.
type APITokenRepository interface {
	// Write inserts the token, or updates its last use and revocation when
	// its ID is stored.
	Write(ctx context.Context, t *APIToken) error
	Get(ctx context.Context, organizationID, tokenID string) (*APIToken, error)
	// GetByHash finds a token by the hash of its secret, in any
	// Organization: authenticating a request is how the Organization is
	// learned. It returns ErrNotFound for none.
	GetByHash(ctx context.Context, hash []byte) (*APIToken, error)
	// ListByPrincipal returns the principal's tokens, newest first.
	ListByPrincipal(ctx context.Context, organizationID, principalID string) ([]*APIToken, error)
}

// WorkloadRepository stores enrolled data-plane workloads.
type WorkloadRepository interface {
	Get(ctx context.Context, organizationID, principalID string) (*Workload, error)
	// GetPlatform returns a platform workload, which belongs to no
	// Organization (ADR 0015).
	GetPlatform(ctx context.Context, principalID string) (*Workload, error)
	// ListPlatform returns every platform workload, by name.
	ListPlatform(ctx context.Context) ([]*Workload, error)
	// Write inserts the workload and its principal, or updates them when the
	// principal ID is stored. It updates the principal's status and the
	// workload's last-seen time.
	Write(ctx context.Context, w *Workload) error
	// UpdateLastSeen writes only the workload's last-seen time, so recording
	// contact never overwrites a concurrent change to its status.
	UpdateLastSeen(ctx context.Context, w *Workload) error
}

// WorkloadEnrollmentTokenRepository stores single-use enrolment tokens.
type WorkloadEnrollmentTokenRepository interface {
	Write(ctx context.Context, t *WorkloadEnrollmentToken) error
	// GetByHashForUpdate locks the token until the transaction ends.
	GetByHashForUpdate(ctx context.Context, hash []byte) (*WorkloadEnrollmentToken, error)
}

// WorkloadCredentialRepository stores how enrolled workloads authenticate.
type WorkloadCredentialRepository interface {
	Write(ctx context.Context, c *WorkloadCredential) error
	Get(ctx context.Context, organizationID, credentialID string) (*WorkloadCredential, error)
	// GetBySecretHash finds a bearer credential by the hash of its token, in
	// any Organization: authenticating a request is how the Organization is
	// learned.
	GetBySecretHash(ctx context.Context, hash []byte) (*WorkloadCredential, error)
	// GetByKindAndIdentifier finds the live certificate or
	// Kubernetes-service-account credential with the identifier.
	GetByKindAndIdentifier(ctx context.Context, kind WorkloadCredentialKind, identifier string) (*WorkloadCredential, error)
	// ListActiveByIdentity returns the Organization's credentials of a kind
	// whose identifier matches and that are not revoked.
	ListActiveByIdentity(ctx context.Context, organizationID string, kind WorkloadCredentialKind, identifier string) ([]*WorkloadCredential, error)
	ListByPrincipal(ctx context.Context, organizationID, principalID string) ([]*WorkloadCredential, error)
	// ListPlatformByPrincipal returns a platform workload's credentials.
	ListPlatformByPrincipal(ctx context.Context, principalID string) ([]*WorkloadCredential, error)
}

// ServiceAccountService manages service accounts, the non-human principals
// CI and other integrations act as, and their API tokens. Roles are granted
// to service accounts like to anyone, through the GrantService. Admins and
// owners manage them; every member may list them.
type ServiceAccountService interface {
	CreateServiceAccount(ctx context.Context, actor identity.Actor, in CreateServiceAccountInput) (*ServiceAccount, error)
	ListServiceAccounts(ctx context.Context, actor identity.Actor, organizationID string, includeInactive bool) ([]*ServiceAccount, error)
	GetServiceAccount(ctx context.Context, actor identity.Actor, organizationID, serviceAccountID string) (*ServiceAccount, error)
	// SuspendServiceAccount stops every one of its tokens from working
	// until it is reactivated.
	SuspendServiceAccount(ctx context.Context, actor identity.Actor, organizationID, serviceAccountID string) (*ServiceAccount, error)
	ReactivateServiceAccount(ctx context.Context, actor identity.Actor, organizationID, serviceAccountID string) (*ServiceAccount, error)

	// CreateAPIToken issues a token for an active service account. Its
	// secret is returned once and never stored.
	CreateAPIToken(ctx context.Context, actor identity.Actor, in CreateAPITokenInput) (*APIToken, string, error)
	ListAPITokens(ctx context.Context, actor identity.Actor, organizationID, serviceAccountID string) ([]*APIToken, error)
	// RevokeAPIToken ends a token for good. Revoking a revoked one does nothing.
	RevokeAPIToken(ctx context.Context, actor identity.Actor, organizationID, tokenID string) (*APIToken, error)
}

// CreateServiceAccountInput describes a new service account.
type CreateServiceAccountInput struct {
	OrganizationID string
	DisplayName    string
	Description    string
}

// CreateAPITokenInput describes a new API token for a service account.
type CreateAPITokenInput struct {
	OrganizationID   string
	ServiceAccountID string
	// Name tells the token apart, such as "github-actions".
	Name string
	// ExpiresAt is nil for a token that does not expire.
	ExpiresAt *time.Time
}

// PeopleService lists an Organization's people: its users. Agents are
// listed through their Teams, and service accounts and workloads are left
// out. Any member may list them.
type PeopleService interface {
	ListPeople(ctx context.Context, actor identity.Actor, query ListPeopleQuery) (*pagination.Result[Person], error)
}

// PeopleRepository reads an Organization's users with their emails and
// Organization roles. An invalid sort or cursor fails with ErrInvalid.
type PeopleRepository interface {
	ListPeople(ctx context.Context, query ListPeopleQuery) (*pagination.Result[Person], error)
}

// PrincipalService names principals by ID, whatever their kind: users,
// service accounts, Agents, agent Sessions, and workloads. Any member may
// read it: it exposes display names and kinds, as listing people does for
// users, but no emails or credentials.
type PrincipalService interface {
	// ListPrincipals returns the Organization's principals among the query's
	// IDs, in the order the IDs give them. Unknown IDs are left out.
	ListPrincipals(ctx context.Context, actor identity.Actor, query ListPrincipalsQuery) ([]PrincipalSummary, error)
}

// ProfileService tells a signed-in caller who they are. It reads only the
// caller's own records, so it asks the authorizer nothing, but it refuses
// with ErrStatus when the caller's principal, Account, or Organization is
// not active, as the authorizer would.
type ProfileService interface {
	Profile(ctx context.Context, actor identity.Actor, organizationID string) (*Profile, error)
	// Permissions returns which of permissions the actor holds on the
	// resource, as the authorizer decides them, so a frontend offers only
	// what the actor may do. The operations still decide for themselves.
	Permissions(ctx context.Context, actor identity.Actor, organizationID, resourceID string, permissions []string) ([]string, error)
}

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

// PrincipalRepository reads what every principal has, whatever its kind,
// for capabilities that accept several kinds, such as Teams, whose members
// are users or Agents.
type PrincipalRepository interface {
	// Get returns the principal's common state, or ErrNotFound when it is
	// not one of the Organization's principals.
	Get(ctx context.Context, organizationID, principalID string) (PrincipalSnapshot, error)
}

// PrincipalReader reads many principals of an Organization by ID at once,
// whatever their kind. It is PrincipalRepository.Get for a batch, so a page
// can name everyone in one call.
type PrincipalReader interface {
	// ListByIDs returns the principals among principalIDs. IDs that name no
	// principal of the Organization are left out rather than refused. The
	// snapshots carry no Metadata.
	ListByIDs(ctx context.Context, organizationID string, principalIDs []string) ([]PrincipalSnapshot, error)
}

// ListAgentsQuery lists an Organization's Agents. It sorts by display_name
// (the default), slug, or created_at, with the ID breaking ties.
type ListAgentsQuery struct {
	OrganizationID  string
	Search          string
	IncludeInactive bool
	Page            pagination.Query
}

type AgentRepository interface {
	Get(ctx context.Context, organizationID, principalID string) (*Agent, error)
	// GetBySlug returns the Organization's Agent with the slug.
	GetBySlug(ctx context.Context, organizationID, slug string) (*Agent, error)
	// List returns the Organization's Agents, filtered and paged as the
	// query asks. An invalid sort or cursor fails with ErrInvalid.
	List(ctx context.Context, query ListAgentsQuery) (*pagination.Result[*Agent], error)
	// Write inserts the Agent's principal and agent rows together, or
	// updates the principal when its ID is stored. A slug another of the
	// Organization's Agents holds is ErrAlreadyExists.
	Write(ctx context.Context, agent *Agent) error
}

// TeamGroups keeps the IAM group that mirrors each Team, so Teams can
// receive grants (ADR 0007). The team capability calls it in the
// transaction of every change that affects the group, and nothing else
// changes team groups. IAM does not depend on Teams: it only stores the
// group, a GroupTeam group whose external ID is the Team's ID.
type TeamGroups interface {
	// SyncTeamGroup creates the Team's group named name, or renames it, and
	// makes its members exactly memberPrincipalIDs. It is idempotent. A
	// member that is not one of the Organization's principals is
	// ErrNotFound, and a name another group holds is ErrAlreadyExists.
	// It returns the group's ID.
	SyncTeamGroup(ctx context.Context, organizationID, teamID, name string, memberPrincipalIDs []string) (string, error)
}

// TeamGroupSource finds the group that mirrors a Team, so the Team can be
// granted roles. The team capability implements it (team.NewGroupSource),
// syncing the group first, so a Team whose group does not exist yet gets
// one. It joins the transaction its context carries. A Team that is not one
// of the Organization's is ErrNotFound, and an archived one ErrStatus.
type TeamGroupSource interface {
	TeamGroupID(ctx context.Context, organizationID, teamID string) (string, error)
}

// PrincipalTeamSource names the Teams principals belong to, so IAM
// can say who an Agent is accountable to without depending on Teams. The
// team capability implements it (team.NewPrincipalTeamSource). It does not
// authorize: the operation that asks has already been authorized.
type PrincipalTeamSource interface {
	// ListTeams returns each principal's Teams, archived ones included,
	// keyed by principal ID. A principal in no Team has no entry.
	ListTeams(ctx context.Context, organizationID string, principalIDs []string) (map[string][]PrincipalTeam, error)
}

// ResourceRepository reads IAM's resource hierarchy.
type ResourceRepository interface {
	// GetResource returns the resource, archived or not, or ErrNotFound.
	GetResource(ctx context.Context, organizationID, resourceID string) (Resource, error)
	// ResourceAncestors returns the IDs of the resource and the resources
	// above it, from the resource up to the Organization.
	ResourceAncestors(ctx context.Context, organizationID, resourceID string) ([]string, error)
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

// HeldResourceReader finds where a principal holds a permission.
type HeldResourceReader interface {
	// HeldResources returns the resources on which the principal, or a
	// group it is in, has an unexpired grant of a role that includes the
	// permission, leaving out archived resources. The permission also
	// applies beneath each one. It does not check that the principal or
	// the Organization is active.
	HeldResources(ctx context.Context, q HeldResourcesQuery) ([]string, error)
}

// ContactReader finds the people other capabilities tell about work, such
// as notifications. It does not authorize: the caller decides who is told.
type ContactReader interface {
	// ActiveUsers returns the active users among the principals, with
	// their emails, ordered by principal ID. Other principals are left out.
	ActiveUsers(ctx context.Context, organizationID string, principalIDs []string) ([]Contact, error)
	// Administrators returns the active users holding the owner or admin
	// role on the Organization itself, directly or through a group,
	// unexpired, ordered by principal ID.
	Administrators(ctx context.Context, organizationID string) ([]Contact, error)
}

// AgentSessionRepository reads agent Sessions. Orchestration will create
// them; nothing writes them yet.
type AgentSessionRepository interface {
	// Get returns the agent Session whose principal is principalID, revoked
	// or not, or ErrNotFound.
	Get(ctx context.Context, organizationID, principalID string) (*AgentSession, error)
}

type GrantRepository interface {
	// ExistsForRole reports whether anyone holds the role on the resource,
	// such as whether an Organization has an owner.
	ExistsForRole(ctx context.Context, organizationID, roleID, resourceID string) (bool, error)
	// Write inserts the Grant; grants never change.
	Write(ctx context.Context, g *Grant) error
	// Get returns the Grant, or ErrNotFound.
	Get(ctx context.Context, organizationID, grantID string) (*Grant, error)
	// GetForUpdate returns the Grant locked until the transaction ends.
	GetForUpdate(ctx context.Context, organizationID, grantID string) (*Grant, error)
	// Delete deletes the Grant.
	Delete(ctx context.Context, g *Grant) error
	// ListGrants lists grants with their grantees named. An invalid sort or
	// cursor fails with ErrInvalid.
	ListGrants(ctx context.Context, query ListGrantsQuery) (*pagination.Result[GrantEntry], error)
	// CountOwners counts the active users that hold the owner role on the
	// Organization directly, unexpired at at.
	CountOwners(ctx context.Context, organizationID string, at time.Time) (int, error)
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
