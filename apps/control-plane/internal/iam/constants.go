package iam

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxNameLength         = 255
	maxEmailLength        = 320
	maxDomainLength       = 253
	maxSubjectLength      = 512
	maxTokenPrefixLength  = 16
	maxPoolLength         = 255
	maxExternalIDLength   = 255
	maxClientIDLength     = 2048
	maxKeyIDLength        = 255
	maxReferenceLength    = 30
	maxMetadataEntries    = 50
	maxRedirectURIs       = 20
	maxScopes             = 50
	minCodeChallengeChars = 43
	maxCodeChallengeChars = 128
)

// Aggregate types name the domain objects that raise events. An envelope
// carries the type and ID of the object whose change raised it.
const (
	AggregateOrganization       = "iam.Organization"
	AggregateAccount            = "iam.Account"
	AggregatePasswordCredential = "iam.PasswordCredential"
	AggregateUser               = "iam.User"
	AggregateAgent              = "iam.Agent"
	AggregateIdentityProvider   = "iam.IdentityProvider"
	AggregateExternalIdentity   = "iam.ExternalIdentity"
	AggregateAccountIdentity    = "iam.AccountIdentity"
	AggregateGrant              = "iam.Grant"
	AggregateOAuthClient        = "iam.OAuthClient"
	AggregateAuthorizationCode  = "iam.OAuthAuthorizationCode"
	AggregateRefreshToken       = "iam.OAuthRefreshToken"
)

// Permissions IAM asks the authorizer about for the Organization itself,
// checked on its root resource. Every built-in role reads it; owners and
// admins manage it.
const (
	PermissionOrganizationRead   = "organization.read"
	PermissionOrganizationManage = "organization.manage"
	// PermissionUserRead lists the Organization's people, with their emails.
	PermissionUserRead = "user.read"
	// PermissionGrantRead lists the grants on a resource; PermissionGrantManage
	// grants and removes roles on it. Granting or removing the owner role
	// also needs PermissionOwnerManage, which only owners hold.
	PermissionGrantRead   = "grant.read"
	PermissionGrantManage = "grant.manage"
)

// Actions name what an actor asks the authorizer to allow. Policy rules and
// approval requests match on them, and they appear in its decisions.
const (
	ActionOrganizationRead   = "organization.read"
	ActionOrganizationUpdate = "organization.update"
	ActionPeopleList         = "people.list"
	ActionGrantList          = "grant.list"
	ActionGrantCreate        = "grant.create"
	ActionGrantRemove        = "grant.remove"
)

// Audit event types IAM emits.
const (
	EventOrganizationUpdated = "organization.updated"
	EventGrantCreated        = "grant.created"
	EventGrantRemoved        = "grant.removed"
)
