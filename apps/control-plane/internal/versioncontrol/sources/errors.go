package sources

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// translateToHttpError turns a service error into the HTTP error the API
// returns. Anything unexpected is logged and becomes a 500 with no detail.
func translateToHttpError(ctx context.Context, err error) error {
	var invalid *versioncontrol.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError

	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, versioncontrol.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, versioncontrol.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, versioncontrol.ErrAlreadyExists):
		return huma.Error409Conflict("this repository is already connected")
	case errors.Is(err, versioncontrol.ErrArchived):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, versioncontrol.ErrConflict):
		return huma.Error409Conflict("changed since it was read; read it again")
	case errors.Is(err, versioncontrol.ErrInactiveConnection):
		return huma.Error409Conflict("the connection is not active")
	case errors.Is(err, versioncontrol.ErrLookupInDataPlane):
		return huma.Error409Conflict("this connection's lookups run in the data plane, so the control plane cannot read its repositories")
	case errors.Is(err, versioncontrol.ErrProviderUnsupported):
		return huma.Error422UnprocessableEntity("no provider is configured for this connection")
	case errors.Is(err, versioncontrol.ErrProviderUnavailable):
		return huma.Error502BadGateway("the provider is unavailable")
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
