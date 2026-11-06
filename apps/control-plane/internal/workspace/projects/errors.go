package projects

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// httpError turns a service error into the HTTP error the API returns. The
// domain's refusals carry messages it wrote, which are safe to show.
// Anything unexpected is logged and becomes a 500 with no detail, since its
// message may describe internals such as SQL.
func httpError(ctx context.Context, err error) error {
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.Is(err, workspace.ErrInvalid),
		errors.Is(err, workspace.ErrInactiveTeam):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, workspace.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, workspace.ErrSlugTaken):
		return huma.Error409Conflict("the slug is already in use")
	case errors.Is(err, workspace.ErrArchived),
		errors.Is(err, workspace.ErrConflict),
		errors.Is(err, workspace.ErrDefaultWorkspace),
		errors.Is(err, workspace.ErrHasDependents):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
