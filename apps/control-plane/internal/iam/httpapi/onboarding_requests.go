package httpapi

type OnboardingHttpRequest struct {
	Body struct {
		DisplayName             string  `json:"display_name" required:"true" minLength:"2" doc:"The display name of the user"`
		Email                   string  `json:"email" required:"true" format:"email" doc:"The email of the user"`
		Password                string  `json:"password" required:"true" minLength:"8" doc:"The password of the user"`
		OrganizationName        string  `json:"organization_name" required:"true" minLength:"2" doc:"The legal name of the organization"`
		OrganizationDisplayName string  `json:"organization_display_name" required:"true" minLength:"2" doc:"The display name of the organization"`
		OrganizationSlug        *string `json:"organization_slug" required:"false" minLength:"2" doc:"The slug of the organization"`
		OrganizationLogoURL     *string `json:"organization_logo_url" required:"false" format:"uri" doc:"The URL of the organization's logo"`
	}
}
