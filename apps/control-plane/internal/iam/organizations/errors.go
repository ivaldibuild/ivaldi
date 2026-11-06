package organizations

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail,
// since its message may describe internals such as SQL.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *iam.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError

	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, iam.ErrAlreadyExists):
		return huma.Error409Conflict("another organization uses the slug")
	case errors.Is(err, iam.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, iam.ErrStatus):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
