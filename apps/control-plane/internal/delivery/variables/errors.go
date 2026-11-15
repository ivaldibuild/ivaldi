package variables

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
	case errors.Is(err, delivery.ErrAmbiguous):
		return huma.Error409Conflict("more than one active Application has that slug; use its ID")
	case errors.Is(err, delivery.ErrSlugTaken):
		return huma.Error409Conflict("the owner already has a variable with that name")
	case errors.Is(err, delivery.ErrConflict):
		return huma.Error409Conflict("the variable changed since it was read; read it and try again")
	case errors.Is(err, delivery.ErrArchived):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, delivery.ErrWorkspaceMismatch):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
