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
