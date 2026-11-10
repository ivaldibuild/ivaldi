// Package webhookroutes serves the platform-wide webhook route lookup the
// gateway uses before it publishes an event. It is a deployment-to-deployment
// endpoint outside the public /v1 API, authenticated by a shared token.
package webhookroutes

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// TokenHeader is the header carrying the deployment lookup token.
const TokenHeader = "X-Ivaldi-Gateway-Token"

// Controller serves GET /internal/webhook-routes/{route/key}.
type Controller struct {
	connections versioncontrol.ConnectionRepository
	token       []byte
	logger      logging.Logger
}

var _ api.Handlers = (*Controller)(nil)

// New returns a Controller, or an error naming a missing dependency.
func New(connections versioncontrol.ConnectionRepository, token string, logger logging.Logger) (*Controller, error) {
	switch {
	case connections == nil:
		return nil, errors.New("webhook routes: a ConnectionRepository is required")
	case token == "":
		return nil, errors.New("webhook routes: a lookup token is required")
	case logger == nil:
		return nil, errors.New("webhook routes: a logger is required")
	}
	return &Controller{connections: connections, token: []byte(token), logger: logger}, nil
}

// RegisterHandlers adds the lookup to the API's mux.
func (c *Controller) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle("GET /internal/webhook-routes/{route_key}", c)
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(TokenHeader)), c.token) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "a valid gateway token is required"})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
