// Package webhookroutes serves the platform-wide webhook route lookup the
// gateway uses before it publishes an event. It is outside the public /v1
// API: only a platform workload whose component holds the permission, the
// gateway, may call it (ADR 0015).
package webhookroutes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Controller serves GET /internal/webhook-routes/{route_key}.
type Controller struct {
	connections versioncontrol.ConnectionRepository
	verifier    identity.Verifier
	authorizer  authorization.Authorizer
	logger      logging.Logger
}

var _ api.Handlers = (*Controller)(nil)

// New returns a Controller, or an error naming a missing dependency. The
// verifier is the API's chain, which authenticates platform workloads by the
// assertions they sign with their keys.
func New(connections versioncontrol.ConnectionRepository, verifier identity.Verifier, authorizer authorization.Authorizer, logger logging.Logger) (*Controller, error) {
	switch {
	case connections == nil:
		return nil, errors.New("webhook routes: a ConnectionRepository is required")
	case verifier == nil:
		return nil, errors.New("webhook routes: a Verifier is required")
	case authorizer == nil:
		return nil, errors.New("webhook routes: an Authorizer is required")
	case logger == nil:
		return nil, errors.New("webhook routes: a logger is required")
	}
	return &Controller{connections: connections, verifier: verifier, authorizer: authorizer, logger: logger}, nil
}

// RegisterHandlers adds the lookup to the API's mux.
func (c *Controller) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /internal/webhook-routes/{route_key}", c)
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	credential, ok := workloadKeyCredential(r)
	if !ok {
		unauthorized(w, "a platform workload's signed assertion is required")
		return
	}
	claims, err := c.verifier.Verify(r.Context(), credential)
	if errors.Is(err, identity.ErrInvalidToken) || errors.Is(err, identity.ErrTokenExpired) {
		unauthorized(w, "the credential is not valid")
		return
	}
	if err != nil {
		c.logger.Error("verify webhook route caller", logging.F("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the caller could not be verified"})
		return
	}
	caller := identity.CallerOf(claims)
	decision, err := c.authorizer.Authorize(r.Context(), authorization.Request{
		Actor:      caller.Actor,
		Permission: versioncontrol.PermissionWebhookRouteResolve,
		ResourceID: authorization.PlatformResource,
		Action:     versioncontrol.ActionWebhookRouteResolve,
	})
	if err != nil {
		c.logger.Error("authorize webhook route lookup", logging.F("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the lookup could not be authorized"})
		return
	}
	if decision.Outcome != authorization.OutcomeAllow {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "the caller may not resolve webhook routes"})
		return
	}

	routeKey := r.PathValue("route_key")
	connection, err := c.connections.GetConnectionByWebhookRouteKey(r.Context(), routeKey)
	if errors.Is(err, versioncontrol.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no webhook route has that key"})
		return
	}
	if err != nil {
		c.logger.Error("webhook route lookup failed",
			logging.F("route_key", routeKey),
			logging.F("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the route lookup failed"})
		return
	}
	writeJSON(w, http.StatusOK, vcs.WebhookRoute{
		OrganizationID: connection.OrganizationID(),
		ConnectionID:   connection.ID(),
		Provider:       connection.Provider(),
		HostURL:        connection.HostURL(),
	})
}

// workloadKeyCredential returns the request's workload key assertion as a
// credential for the verifier. Only that kind of credential is accepted
// here, and a bearer token already in an internal encoding is refused.
func workloadKeyCredential(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || identity.HasInternalCredentialPrefix(token) {
		return "", false
	}
	if !strings.EqualFold(r.Header.Get(identity.CredentialHeader), identity.CredentialHeaderWorkloadKey) {
		return "", false
	}
	return identity.WorkloadKeyCredential(token), true
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
