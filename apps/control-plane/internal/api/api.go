// Package api serves the control plane's HTTP API. It owns what every
// capability shares: the listener, the OpenAPI document, middleware, and
// shutdown. It owns no capability's operations: each capability's httpapi
// package registers its own through Routes, and cmd/server passes them in, so
// this package imports no capability.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
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

// Public is the Metadata key that serves an operation without an access
// token: Metadata: map[string]any{api.Public: true}. New refuses an operation
// that declares neither Security nor Public, so an operation cannot be left
// open by forgetting to secure it.
const Public = "ivaldi.public"

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
	webUI    http.Handler
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

// WithWebUI serves the web app from the same origin as the API, on every path
// no API handler claimed. Without it the control plane serves no web app.
func WithWebUI(h http.Handler) Option {
	return func(o *options) { o.webUI = h }
}

// Server is the control plane's HTTP API.
type Server struct {
	http    *http.Server
	openapi *huma.OpenAPI
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
	var clientCAs *x509.CertPool
	if cfg.TLS.ClientCAFile != "" {
		if cfg.TLS.CertFile == "" {
			return nil, errors.New("api: a client CA needs TLS on the listener")
		}
		pem, err := os.ReadFile(cfg.TLS.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("api: read client CA: %w", err)
		}
		clientCAs = x509.NewCertPool()
		if !clientCAs.AppendCertsFromPEM(pem) {
			return nil, errors.New("api: client CA file has no PEM certificates")
		}
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
	hcfg.Transformers = append(hcfg.Transformers, emptyLists)
	hapi := humago.New(mux, hcfg)

	// Everything under BasePath that no operation claimed is answered by the
	// API, in its problem format. Without this, a mistyped endpoint would fall
	// through to the web app and answer JSON with an HTML page.
	for _, m := range apiMethods {
		mux.Handle(m+" "+BasePath+"/", apiFallback(mux, m))
	}

	// Registered last, so every API path wins: the mux prefers the more
	// specific pattern, and the web app only gets what is left.
	if o.webUI != nil {
		mux.Handle("GET /", o.webUI)
	}
	hapi.UseMiddleware(withRequest(logger), authenticate(hapi, o.verifier, logger, resource+protectedResourcePath))
	v1 := huma.NewGroup(hapi, BasePath)
	var open []string
	v1.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
		if len(op.Security) == 0 && op.Metadata[Public] != true {
			open = append(open, op.Method+" "+op.Path)
		}
		next(op)
	})

	for _, r := range o.routes {
		r.RegisterRoutes(v1)
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("api: operations must require an access token or be marked api.Public: %s", strings.Join(open, ", "))
	}

	srv := &Server{
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(*cfg.Port)),
			Handler:           recoverPanics(logger, withClientCertificate(mux)),
			ReadHeaderTimeout: 10 * time.Second,
		},
		openapi: hapi.OpenAPI(),
		logger:  logger,
		tls:     cfg.TLS,
		timeout: cfg.ShutdownTimeout,
	}
	if clientCAs != nil {
		srv.http.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ClientAuth: tls.VerifyClientCertIfGiven,
			ClientCAs:  clientCAs,
		}
	}
	return srv, nil
}

// OpenAPI returns the API's description.
func (s *Server) OpenAPI() *huma.OpenAPI { return s.openapi }

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
		credential, err := requestCredential(ctx)
		switch {
		case errors.Is(err, errNoCredential):
			refuse(ctx, challenge, "a credential is required")
			return
		case err != nil:
			refuse(ctx, challenge+`, error="invalid_token"`, "the access token is not valid")
			return
		}
		claims, err := v.Verify(ctx.Context(), credential)
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

var (
	errNoCredential       = errors.New("no credential")
	errReservedCredential = errors.New("a bearer token uses a reserved credential prefix")
)

// requestCredential returns the credential the authenticator verifies: a
// bearer token when present, otherwise a verified client certificate.
//
// The verifier trusts the internal mTLS, Kubernetes, and workload-key
// encodings because only this function builds them, so a bearer token that
// already carries one of their prefixes is refused rather than passed on: otherwise a client could
// claim any certificate fingerprint without presenting the certificate.
func requestCredential(ctx huma.Context) (string, error) {
	if token, ok := bearerToken(ctx.Header("Authorization")); ok {
		if identity.HasInternalCredentialPrefix(token) {
			return "", errReservedCredential
		}
		switch kind := ctx.Header(identity.CredentialHeader); {
		case strings.EqualFold(kind, identity.CredentialHeaderKubernetes):
			return identity.KubernetesCredential(token), nil
		case strings.EqualFold(kind, identity.CredentialHeaderWorkloadKey):
			return identity.WorkloadKeyCredential(token), nil
		}
		return token, nil
	}
	if cert := ClientCertificate(ctx.Context()); cert != nil {
		sum := sha256.Sum256(cert.Raw)
		return identity.MTLSCredential(hex.EncodeToString(sum[:]), cert.NotAfter), nil
	}
	return "", errNoCredential
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

// apiMethods are the methods API operations use. Each gets its own fallback
// pattern: one pattern for every method would overlap the web app's "GET /"
// with neither more specific, which the mux refuses.
var apiMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// apiFallback answers a request under BasePath that no operation matched, in
// the problem format every other API error uses: 405 with Allow when the path
// is an operation under another method, as the mux would say without the
// fallback, and 404 otherwise.
func apiFallback(mux *http.ServeMux, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var allow []string
		for _, m := range apiMethods {
			if m == method {
				continue
			}
			probe := &http.Request{Method: m, Host: r.Host, URL: r.URL, Header: r.Header}
			if _, pattern := mux.Handler(probe); pattern != "" && pattern != m+" "+BasePath+"/" {
				allow = append(allow, m)
			}
		}
		if len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			writeProblem(w, http.StatusMethodNotAllowed, "This operation does not accept "+r.Method+".")
			return
		}
		writeProblem(w, http.StatusNotFound, "No operation is served at this path.")
	}
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
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
