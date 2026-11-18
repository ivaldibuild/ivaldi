package inbox

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// httpError turns a service error into the HTTP error the API returns.
// Anything unexpected is logged and becomes a 500 with no detail.
func httpError(ctx context.Context, err error) error {
	var invalid *notifications.ErrInvalidInput
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, notifications.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, authorization.ErrDenied), errors.Is(err, authorization.ErrApprovalRequired):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
