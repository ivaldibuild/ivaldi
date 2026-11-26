package grants

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail.
func httpError(ctx context.Context, err error) error {
	var deliveryInvalid *delivery.ErrInvalidInput
	var invalid *credentials.ErrInvalidInput
	switch {
	case errors.As(err, &deliveryInvalid):
		return huma.Error422UnprocessableEntity(deliveryInvalid.Message)
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrNotClusterAgent), errors.Is(err, delivery.ErrNotDeploymentAgent):
		return huma.Error403Forbidden("the caller does not serve this deployment's cluster")
	case errors.Is(err, delivery.ErrNotFound), errors.Is(err, credentials.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, delivery.ErrDeploymentStatus):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, credentials.ErrStoreRefused), errors.Is(err, credentials.ErrNoCredential):
		// The message names the store and what it answered, never a
		// credential.
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, credentials.ErrStoreUnavailable):
		return huma.Error503ServiceUnavailable(err.Error())
	}
	return api.InternalError(ctx, err)
}
