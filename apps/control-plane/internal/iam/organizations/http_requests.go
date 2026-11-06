package organizations

import "github.com/ivaldibuild/ivaldi-control-plane/internal/iam"

type UpdateOrganizationRequest struct {
	Body struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"1" maxLength:"255" doc:"New display name, shown across Ivaldi"`
		Name        *string `json:"name,omitempty" required:"false" minLength:"1" maxLength:"255" doc:"New full name, such as the legal name"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New address, unique across the platform. Hints and links that name the old slug stop matching."`
		LogoURL     *string `json:"logo_url,omitempty" required:"false" doc:"New https logo URL; empty removes it"`
	}
}

// props leaves out the Organization's metadata, which this API does not change.
func (r *UpdateOrganizationRequest) props() iam.UpdateOrganizationProps {
	return iam.UpdateOrganizationProps{
		DisplayName: r.Body.DisplayName,
		Name:        r.Body.Name,
		Slug:        r.Body.Slug,
		LogoURL:     r.Body.LogoURL,
	}
}
