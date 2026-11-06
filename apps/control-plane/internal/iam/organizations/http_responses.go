package organizations

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type OrganizationResponse struct {
	Body api.Response[Organization]
}

// Organization is the caller's Organization. Its metadata and status are
// left out: an inactive Organization cannot be read through the API.
type Organization struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	LogoURL     *string   `json:"logo_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func organizationResponse(o *iam.Organization) *OrganizationResponse {
	return &OrganizationResponse{Body: api.SuccessResponse(nil, Organization{
		ID:          o.ID(),
		Slug:        o.Slug(),
		Name:        o.Name(),
		DisplayName: o.DisplayName(),
		LogoURL:     o.LogoURL(),
		CreatedAt:   o.CreatedAt(),
		UpdatedAt:   o.UpdatedAt(),
	})}
}
