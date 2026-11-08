package changes

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *provenance.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError

	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, provenance.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, provenance.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, provenance.ErrAlreadyExists):
		return huma.Error409Conflict("a commit in this change is already recorded")
	case errors.Is(err, provenance.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
