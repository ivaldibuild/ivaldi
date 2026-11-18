package approvals

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail, since its
// message may describe internals such as SQL.
func httpError(ctx context.Context, err error) error {
	var invalid *policy.ErrInvalidInput
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, policy.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, policy.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, policy.ErrNotPending), errors.Is(err, policy.ErrSessionRevoked), errors.Is(err, policy.ErrCannotApply):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, authorization.ErrDenied), errors.Is(err, authorization.ErrApprovalRequired):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
