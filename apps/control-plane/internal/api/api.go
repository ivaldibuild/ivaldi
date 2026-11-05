// Package api serves the control plane's HTTP API. It owns what every
// capability shares: the listener, the OpenAPI document, middleware, and
// shutdown. It owns no capability's operations: each capability's httpapi
// package registers its own through Routes, and cmd/server passes them in, so
// this package imports no capability.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// BasePath prefixes every capability's operations.
const BasePath = "/v1"

// BearerAuth is the security scheme an operation names to require an access
// token: Security: []map[string][]string{{api.BearerAuth: {}}}.
const BearerAuth = "bearer"

// protectedResourcePath serves the API's protected resource metadata (RFC
// 9728), which tells clients where to get a token.
const protectedResourcePath = "/.well-known/oauth-protected-resource"

// Routes registers a capability's operations on the API. Paths are relative
// to BasePath.
type Routes interface {
	RegisterRoutes(api huma.API)
}

// Handlers registers plain HTTP handlers outside BasePath, for endpoints
// whose formats an OpenAPI operation cannot describe, such as OAuth's form
// posts and redirects.
type Handlers interface {
	RegisterHandlers(mux *http.ServeMux)
}

// Option configures New.
type Option func(*options)

type options struct {
	routes   []Routes
	handlers []Handlers
	verifier identity.Verifier
}

// WithRoutes adds capabilities' operations.
func WithRoutes(r ...Routes) Option {
	return func(o *options) { o.routes = append(o.routes, r...) }
}

// WithHandlers adds plain HTTP handlers.
func WithHandlers(h ...Handlers) Option {
	return func(o *options) { o.handlers = append(o.handlers, h...) }
}

// WithVerifier verifies the access tokens of operations that require one.
// Without it, such operations are refused.
func WithVerifier(v identity.Verifier) Option {
	return func(o *options) { o.verifier = v }
}

// Server is the control plane's HTTP API.
type Server struct {
	http    *http.Server
	logger  logging.Logger
	tls     config.TLS
	timeout time.Duration
}

// New builds the API from cfg and the options. version is the binary's,
// published in the OpenAPI document.
func New(cfg *config.Config, version string, logger logging.Logger, opts ...Option) (*Server, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
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

	resource := cfg.IssuerURL()
	if o.verifier != nil {
		mux.HandleFunc("GET "+protectedResourcePath, protectedResourceMetadata(resource))
	}
	for _, h := range o.handlers {
		h.RegisterHandlers(mux)
	}

	hcfg := huma.DefaultConfig("Ivaldi Control Plane API", version)
	hcfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		BearerAuth: {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
	}
	hapi := humago.New(mux, hcfg)
	hapi.UseMiddleware(authenticate(hapi, o.verifier, logger, resource+protectedResourcePath))
	v1 := huma.NewGroup(hapi, BasePath)

	for _, r := range o.routes {
		r.RegisterRoutes(v1)
	}

	return &Server{
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(*cfg.Port)),
			Handler:           recoverPanics(logger, cors(cfg.AllowedOrigins, mux)),
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

// authenticate verifies the bearer token of an operation that declares
// Security, and puts the caller it names in the request's context. It fails
// closed: without a verifier, such operations are refused. Operations that
// declare no Security are served to anyone.
func authenticate(api huma.API, v identity.Verifier, logger logging.Logger, metadataURL string) func(huma.Context, func(huma.Context)) {
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, metadataURL)
	refuse := func(ctx huma.Context, header, msg string) {
		ctx.SetHeader("WWW-Authenticate", header)
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, msg)
	}
	return func(ctx huma.Context, next func(huma.Context)) {
		if len(ctx.Operation().Security) == 0 {
			next(ctx)
			return
		}
		if v == nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication is not available")
			return
		}
		token, ok := bearerToken(ctx.Header("Authorization"))
		if !ok {
			refuse(ctx, challenge, "an access token is required")
			return
		}
		claims, err := v.Verify(ctx.Context(), token)
		switch {
		case errors.Is(err, identity.ErrTokenExpired):
			refuse(ctx, challenge+`, error="invalid_token", error_description="the access token expired"`, "the access token expired")
			return
		case errors.Is(err, identity.ErrInvalidToken):
			refuse(ctx, challenge+`, error="invalid_token"`, "the access token is not valid")
			return
		case err != nil:
			logger.Error("verify access token", logging.F("error", err))
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "could not verify the access token")
			return
		}
		next(huma.WithContext(ctx, identity.WithCaller(ctx.Context(), identity.CallerOf(claims))))
	}
}

// bearerToken returns the token from an Authorization header (RFC 6750).
func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// protectedResourceMetadata describes the API as an OAuth protected resource
// (RFC 9728): its own control plane is its authorization server.
func protectedResourceMetadata(resource string) http.HandlerFunc {
	body, _ := json.Marshal(map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{resource},
		"bearer_methods_supported": []string{"header"},
	})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body)
	}
}

// cors lets the listed browser origins, such as the web app's, call the API.
// Requests carry tokens in headers or bodies, never cookies, so credentials
// are not allowed.
func cors(origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !allowed[origin] {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.Set("Access-Control-Expose-Headers", "WWW-Authenticate")
		next.ServeHTTP(w, r)
	})
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
