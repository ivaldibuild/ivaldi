package iam

// ListPrincipalsQuery names principals by ID, whatever their kind.
type ListPrincipalsQuery struct {
	OrganizationID string
	// IDs are the principals to name. IDs that name no principal of the
	// Organization, and those of another Organization, are left out rather
	// than refused, so a list does not fail because one principal was
	// deleted.
	IDs []string
}

// PrincipalSummary names a principal, whatever its kind, so a page can show
// who created or acted on something without asking each kind's capability.
type PrincipalSummary struct {
	Principal PrincipalSnapshot
	// Teams are the active Teams an Agent belongs to, by display name. Every
	// other kind has none.
	Teams []PrincipalTeam
}

// PrincipalTeam is one Team a principal belongs to. IAM keeps the name
// rather than the team capability's type, so IAM does not depend on Teams.
type PrincipalTeam struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}
