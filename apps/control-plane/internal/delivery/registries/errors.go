package registries

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail, since its
// message may describe internals such as SQL.
func httpError(ctx context.Context, err error) error {
	var invalid *delivery.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, delivery.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, delivery.ErrSlugTaken):
		return huma.Error409Conflict("the slug is already in use")
	case errors.Is(err, delivery.ErrAlreadyExists),
		errors.Is(err, delivery.ErrArchived),
		errors.Is(err, delivery.ErrInUse):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
