package iam

type OnboardOrganizationInput struct {
	OwnerEmail              string
	OwnerDisplayName        string
	OwnerPassword           string
	OrganizationName        string
	OrganizationDisplayName string
	OrganizationLogoURL     *string
	OrganizationSlug        *string
}

func (i *OnboardOrganizationInput) Validate() error {
	if i.OwnerEmail == "" {
		return NewErrInvalidInput("owner email is required")
	}

	if i.OwnerDisplayName == "" {
		return NewErrInvalidInput("owner display name is required")
	}

	if i.OwnerPassword == "" {
		return NewErrInvalidInput("owner password is required")
	}

	if i.OrganizationName == "" {
		return NewErrInvalidInput("organization name is required")
	}

	if i.OrganizationDisplayName == "" {
		return NewErrInvalidInput("organization display name is required")
	}

	return nil
}
