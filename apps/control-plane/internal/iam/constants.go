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
	AggregateIdentityProvider   = "iam.IdentityProvider"
	AggregateExternalIdentity   = "iam.ExternalIdentity"
	AggregateAccountIdentity    = "iam.AccountIdentity"
	AggregateGrant              = "iam.Grant"
	AggregateOAuthClient        = "iam.OAuthClient"
	AggregateAuthorizationCode  = "iam.OAuthAuthorizationCode"
	AggregateRefreshToken       = "iam.OAuthRefreshToken"
)
