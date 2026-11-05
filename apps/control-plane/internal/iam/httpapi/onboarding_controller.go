package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
)

type OnboardingController struct {
	service iam.Onboarding
}

func NewOnboardingController(service iam.Onboarding) *OnboardingController {
	return &OnboardingController{
		service: service,
	}
}

func (c *OnboardingController) RegisterRoutes(h huma.API) {
	huma.Register(h, huma.Operation{
		OperationID:   "onboarding",
		Method:        http.MethodPost,
		Path:          "/onboarding",
		Summary:       "Onboarding an organization via email",
		Tags:          []string{"onboarding"},
		DefaultStatus: http.StatusCreated,
	}, c.OnboardWithEmail)
}

func (c *OnboardingController) OnboardWithEmail(ctx context.Context, req *OnboardingHttpRequest) (*OnboardingResponse, error) {
	err := c.service.OnboardOrganization(ctx, &iam.OnboardOrganizationInput{
		OwnerEmail:              req.Body.Email,
		OwnerDisplayName:        req.Body.DisplayName,
		OwnerPassword:           req.Body.Password,
		OrganizationName:        req.Body.OrganizationName,
		OrganizationDisplayName: req.Body.OrganizationDisplayName,
		OrganizationLogoURL:     req.Body.OrganizationLogoURL,
		OrganizationSlug:        req.Body.OrganizationSlug,
	})

	if err != nil {
		return nil, c.translateError(err)
	}

	return &OnboardingResponse{}, nil
}

// translateError passes the reason to the caller for conflicts and invalid
// input. Anything else is internal, so the service logs it and the caller
// sees only that it failed.
func (c *OnboardingController) translateError(err error) error {
	if _, ok := errors.AsType[*onboarding.ErrAccountConflict](err); ok {
		return huma.Error409Conflict("failed to complete onboarding", err)
	}
	if _, ok := errors.AsType[*onboarding.ErrOrganizationConflict](err); ok {
		return huma.Error409Conflict("failed to complete onboarding", err)
	}
	if errors.Is(err, iam.ErrInvalid) {
		return huma.Error422UnprocessableEntity("failed to complete onboarding", err)
	}
	return huma.Error500InternalServerError("failed to complete onboarding")
}
