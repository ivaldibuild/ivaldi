// Package api serves the control plane's HTTP API. It owns what every
// capability shares: the listener, the OpenAPI document, middleware, and
// shutdown. It owns no capability's operations: each capability's httpapi
// package registers its own through Routes, and cmd/server passes them in, so
// this package imports no capability.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// BasePath prefixes every capability's operations.
const BasePath = "/v1"

// Routes registers a capability's operations on the API. Paths are relative
// to BasePath.
type Routes interface {
	RegisterRoutes(api huma.API)
}

// Server is the control plane's HTTP API.
type Server struct {
	http    *http.Server
	logger  logging.Logger
	tls     config.TLS
	timeout time.Duration
}

// New builds the API from cfg and the capabilities' routes. version is the
// binary's, published in the OpenAPI document.
func New(cfg *config.Config, version string, logger logging.Logger, routes ...Routes) (*Server, error) {
	if cfg.Port == nil {
		return nil, errors.New("api: port is required")
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return nil, errors.New("api: TLS needs both a certificate and a key file")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	hapi := humago.New(mux, huma.DefaultConfig("Ivaldi Control Plane API", version))
	hapi.UseMiddleware(refuseSecured(hapi))
	v1 := huma.NewGroup(hapi, BasePath)

	for _, r := range routes {
		r.RegisterRoutes(v1)
	}

	return &Server{
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(*cfg.Port)),
			Handler:           recoverPanics(logger, mux),
			ReadHeaderTimeout: 10 * time.Second,
		},
		logger:  logger,
		tls:     cfg.TLS,
		timeout: cfg.ShutdownTimeout,
	}, nil
}

// Handler returns the API's handler, for serving it without a listener.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is done, then gives in-flight requests the shutdown
// timeout to finish.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("api: listen on %s: %w", s.http.Addr, err)
	}

	served := make(chan error, 1)
	go func() {
		if s.tls.CertFile != "" {
			served <- s.http.ServeTLS(ln, s.tls.CertFile, s.tls.KeyFile)
		} else {
			served <- s.http.Serve(ln)
		}
	}()
	s.logger.Info("api listening", logging.F("addr", ln.Addr().String()))

	select {
	case err := <-served:
		return fmt.Errorf("api: serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("api: shutdown: %w", err)
	}
	return nil
}

// refuseSecured fails closed until callers can be authenticated: an operation
// that declares Security is refused rather than served to anyone. Replace it
// with middleware that resolves the caller into an identity.Actor (ADR 0003).
func refuseSecured(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if len(ctx.Operation().Security) > 0 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication is not available")
			return
		}
		next(ctx)
	}
}

// recoverPanics turns a panic in a handler into a 500 and logs it, rather than
// dropping the connection.
func recoverPanics(logger logging.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			logger.Error("panic serving request",
				logging.F("method", r.Method),
				logging.F("path", r.URL.Path),
				logging.F("panic", v),
				logging.F("stack", string(debug.Stack())),
			)
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"title":"Internal Server Error","status":500}`))
		}()
		next.ServeHTTP(w, r)
	})
}
