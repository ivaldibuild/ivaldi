package profile

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// translateToHttpError turns a service error into the HTTP error the API
// returns. A token whose principal or Organization is missing or not active
// is refused as the authorizer refuses it; anything unexpected is logged and
// becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, iam.ErrInvalid), errors.Is(err, iam.ErrNotFound):
		return huma.Error403Forbidden("the token does not name an active member of an Organization")
	case errors.Is(err, iam.ErrStatus):
		return huma.Error403Forbidden("your access to this Organization is not active")
	}
	return api.InternalError(ctx, err)
}
