package iam

// Membership is an Account's User in one Organization, as sign-in lists them
// for the person to choose from. It is a read model, not an aggregate.
type Membership struct {
	OrganizationID     string
	OrganizationSlug   string
	OrganizationName   string
	OrganizationStatus OrganizationStatus
	// PrincipalID is the User's.
	PrincipalID string
	Status      PrincipalStatus
}

// Active reports whether the person can act in the Organization through
// this membership: both the User and the Organization are active.
func (m Membership) Active() bool {
	return m.Status == PrincipalActive && m.OrganizationStatus == OrganizationActive
}

// SignInMethod is how a person proved who they are.
type SignInMethod string

const (
	SignInPassword         SignInMethod = "password"
	SignInIdentityProvider SignInMethod = "identity_provider"
	SignInPlatform         SignInMethod = "platform"
)

// SignedIn is the outcome of a sign-in: whose Account it is, and where they
// may act.
type SignedIn struct {
	AccountID          string
	Method             SignInMethod
	IdentityProviderID string
	// RestrictedTo is the only Organization the sign-in may act in, when an
	// Organization's identity provider signed the person in.
	RestrictedTo string
	// Memberships are the active ones the person may choose from, oldest
	// first.
	Memberships []Membership
}

// Allows reports whether the sign-in may act in the Organization.
func (s *SignedIn) Allows(organizationID string) bool {
	return s.RestrictedTo == "" || s.RestrictedTo == organizationID
}
