package iam

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxNameLength         = 255
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
	AggregateOrganization            = "iam.Organization"
	AggregateAccount                 = "iam.Account"
	AggregatePasswordCredential      = "iam.PasswordCredential"
	AggregateUser                    = "iam.User"
	AggregateAgent                   = "iam.Agent"
	AggregateServiceAccount          = "iam.ServiceAccount"
	AggregateAPIToken                = "iam.APIToken"
	AggregateIdentityProvider        = "iam.IdentityProvider"
	AggregateExternalIdentity        = "iam.ExternalIdentity"
	AggregateAccountIdentity         = "iam.AccountIdentity"
	AggregateGrant                   = "iam.Grant"
	AggregateWorkload                = "iam.Workload"
	AggregateWorkloadCredential      = "iam.WorkloadCredential"
	AggregateWorkloadEnrollmentToken = "iam.WorkloadEnrollmentToken"
	AggregateOAuthClient             = "iam.OAuthClient"
	AggregateAuthorizationCode       = "iam.OAuthAuthorizationCode"
	AggregateRefreshToken            = "iam.OAuthRefreshToken"
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
	// PermissionServiceAccountRead lists service accounts and their tokens'
	// names; PermissionServiceAccountManage creates them, issues and revokes
	// their API tokens, and suspends them.
	PermissionServiceAccountRead   = "service_account.read"
	PermissionServiceAccountManage = "service_account.manage"
	// PermissionAgentRead lists Agents; PermissionAgentManage creates them,
	// changes their identity, and suspends or reactivates them. Agents are
	// IAM principals that can hold grants, so this is separate from
	// managing Teams.
	PermissionAgentRead   = "agent.read"
	PermissionAgentManage = "agent.manage"
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
	// ActionPermissionCheck asks whether the caller holds a permission, so
	// a frontend offers only what the caller may do. It changes nothing.
	ActionPermissionCheck = "permission.check"

	ActionServiceAccountCreate     = "service_account.create"
	ActionServiceAccountList       = "service_account.list"
	ActionServiceAccountRead       = "service_account.read"
	ActionServiceAccountSuspend    = "service_account.suspend"
	ActionServiceAccountReactivate = "service_account.reactivate"
	ActionAPITokenCreate           = "api_token.create"
	ActionAPITokenList             = "api_token.list"
	ActionAPITokenRevoke           = "api_token.revoke"
)

// Audit event types IAM emits.
const (
	EventOrganizationUpdated = "organization.updated"
	EventGrantCreated        = "grant.created"
	EventGrantRemoved        = "grant.removed"

	EventServiceAccountCreated     = "service_account.created"
	EventServiceAccountSuspended   = "service_account.suspended"
	EventServiceAccountReactivated = "service_account.reactivated"
	EventAPITokenCreated           = "api_token.created"
	EventAPITokenRevoked           = "api_token.revoked"

	EventWorkloadEnrollmentCreated  = "workload_enrollment.created"
	EventWorkloadEnrollmentRedeemed = "workload_enrollment.redeemed"
	EventWorkloadCredentialRenewed  = "workload_credential.renewed"
	EventAgentUpdated               = "agent.updated"
	EventAgentSuspended             = "agent.suspended"
	EventAgentReactivated           = "agent.reactivated"
)
