package secretstores

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail, since its
// message may describe internals such as SQL.
func httpError(ctx context.Context, err error) error {
	var invalid *credentials.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, credentials.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, credentials.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, credentials.ErrSlugTaken):
		return huma.Error409Conflict("the slug is already in use")
	case errors.Is(err, credentials.ErrArchived),
		errors.Is(err, credentials.ErrInUse),
		errors.Is(err, credentials.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, credentials.ErrWorkspaceMismatch):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
