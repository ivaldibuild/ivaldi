package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

type requestKey struct{}

// request is what InternalError logs about the request it fails.
type request struct {
	logger      logging.Logger
	method      string
	path        string
	operationID string
}

// withRequest is middleware that lets handlers report unexpected errors
// through InternalError.
func withRequest(logger logging.Logger) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r := request{logger: logger, method: ctx.Method(), path: ctx.URL().Path}
		if op := ctx.Operation(); op != nil {
			r.operationID = op.OperationID
		}
		next(huma.WithValue(ctx, requestKey{}, r))
	}
}

// InternalError logs err, which a handler did not expect, and returns a 500
// that says nothing about it: an unexpected error's message may describe
// internals, such as SQL. Capabilities' error mappings return it for any
// error they do not recognize.
func InternalError(ctx context.Context, err error) huma.StatusError {
	if r, ok := ctx.Value(requestKey{}).(request); ok {
		r.logger.Error("request failed",
			logging.F("method", r.method),
			logging.F("path", r.path),
			logging.F("operation_id", r.operationID),
			logging.F("error", err.Error()))
	}
	return huma.Error500InternalServerError("the request failed")
}
