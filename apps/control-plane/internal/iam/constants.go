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
