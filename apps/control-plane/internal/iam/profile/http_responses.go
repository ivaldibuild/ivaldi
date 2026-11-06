package profile

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type MeResponse struct {
	Body api.Response[Me]
}

// Me is the signed-in caller.
type Me struct {
	Principal    MePrincipal    `json:"principal"`
	Organization MeOrganization `json:"organization"`
	// Roles are the caller's roles on the Organization, granted directly or
	// through a group.
	Roles []MeRole `json:"roles"`
}

type MePrincipal struct {
	ID          string            `json:"id"`
	Kind        iam.PrincipalKind `json:"kind"`
	DisplayName string            `json:"display_name"`
	// Email is null for anything but a user.
	Email *string `json:"email"`
}

type MeOrganization struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	DisplayName string  `json:"display_name"`
	LogoURL     *string `json:"logo_url"`
}

type MeRole struct {
	// Role is the built-in role's key, such as "owner", or empty for a
	// custom role.
	Role       string `json:"role"`
	RoleID     string `json:"role_id"`
	ResourceID string `json:"resource_id"`
}

func meResponse(p *iam.Profile) *MeResponse {
	roles := make([]MeRole, len(p.Roles))
	for i, r := range p.Roles {
		roles[i] = MeRole{Role: r.Key, RoleID: r.RoleID, ResourceID: r.ResourceID}
	}
	return &MeResponse{Body: api.SuccessResponse(nil, Me{
		Principal: MePrincipal{
			ID:          p.Principal.ID,
			Kind:        p.Principal.Kind,
			DisplayName: p.Principal.DisplayName,
			Email:       p.Email,
		},
		Organization: MeOrganization{
			ID:          p.Organization.ID,
			Slug:        p.Organization.Slug,
			DisplayName: p.Organization.DisplayName,
			LogoURL:     p.Organization.LogoURL,
		},
		Roles: roles,
	})}
}
