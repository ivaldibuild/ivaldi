// Package webhook receives provider webhooks, verifies their signatures,
// resolves their platform-wide route, and publishes normalized events.
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Headers GitHub sends with a webhook.
const (
	headerEvent     = "X-GitHub-Event"
	headerDelivery  = "X-GitHub-Delivery"
	headerSignature = "X-Hub-Signature-256"
)

// writeGrace is how long after a delivery's deadline its response may still
// be written, so a delivery that ran out of time still gets its error.
const writeGrace = 5 * time.Second

// Handler receives GitHub webhooks, verifies them, resolves their route, and
// publishes the normalized event.
type Handler struct {
	verifier     SignatureVerifier
	resolver     RouteResolver
	normalizer   *Normalizer
	publisher    events.Publisher
	maxBodyBytes int64
	timeout      time.Duration
	logger       logging.Logger
}

// HandlerConfig holds a Handler's collaborators.
type HandlerConfig struct {
	Verifier     SignatureVerifier
	Resolver     RouteResolver
	Normalizer   *Normalizer
	Publisher    events.Publisher
	MaxBodyBytes int64
	// Timeout bounds one delivery: reading its body, resolving its route,
	// and publishing it.
	Timeout time.Duration
	Logger  logging.Logger
}

// NewHandler returns a Handler, or an error naming a missing dependency.
func NewHandler(c HandlerConfig) (*Handler, error) {
	switch {
	case c.Verifier == nil:
		return nil, errors.New("webhook handler: a signature verifier is required")
	case c.Resolver == nil:
		return nil, errors.New("webhook handler: a route resolver is required")
	case c.Normalizer == nil:
		return nil, errors.New("webhook handler: a normalizer is required")
	case c.Publisher == nil:
		return nil, errors.New("webhook handler: a publisher is required")
	case c.MaxBodyBytes <= 0:
		return nil, errors.New("webhook handler: the body limit must be positive")
	case c.Timeout <= 0:
		return nil, errors.New("webhook handler: the timeout must be positive")
	case c.Logger == nil:
		return nil, errors.New("webhook handler: a logger is required")
	}
	return &Handler{
		verifier:     c.Verifier,
		resolver:     c.Resolver,
		normalizer:   c.Normalizer,
		publisher:    c.Publisher,
		maxBodyBytes: c.MaxBodyBytes,
		timeout:      c.Timeout,
		logger:       c.Logger,
	}, nil
}

// ServeHTTP bounds the delivery itself. The gateway's server sets no read or
// write timeout, because other modules, such as MCP, keep connections open
// for long streams.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deadline := time.Now().Add(h.timeout)
	// A ResponseWriter that cannot take deadlines, such as a test recorder,
	// reports http.ErrNotSupported; the context below still bounds the work.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline.Add(writeGrace))
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	h.serve(w, r.WithContext(ctx))
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "the request body is too large")
			return
		}
		h.logger.Warn("webhook body could not be read", logging.F("error", err.Error()))
		writeError(w, http.StatusBadRequest, "the request body could not be read")
		return
	}
	// Verify before parsing, so a caller without the secret learns nothing
	// about the payload.
	if err := h.verifier.Verify(r.Header.Get(headerSignature), body); err != nil {
		writeError(w, http.StatusUnauthorized, "the webhook signature is not valid")
		return
	}

	eventName := r.Header.Get(headerEvent)
	deliveryID := r.Header.Get(headerDelivery)
	if eventName == "" || deliveryID == "" {
		writeError(w, http.StatusBadRequest, "the webhook event and delivery headers are required")
		return
	}
	// Events we do not handle, such as ping, are acknowledged without a
	// route: they may not name an installation at all.
	if eventName != eventRepository && eventName != eventInstallation {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var payload githubWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "the webhook payload is not valid JSON")
		return
	}
	installationID := payload.installationID()
	if installationID == "" {
		writeError(w, http.StatusBadRequest, "the webhook payload names no installation")
		return
	}

	// Normalize before resolving the route, so an action the gateway does not
	// publish (such as installation.created, which arrives before its
	// Connection exists) is acknowledged without a lookup.
	routeKey := h.normalizer.RouteKey(installationID)
	event, handled, err := h.normalizer.Normalize(eventName, payload, routeKey, deliveryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !handled {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	route, err := h.resolver.Resolve(r.Context(), routeKey)
	if errors.Is(err, ErrRouteNotFound) {
		writeError(w, http.StatusNotFound, "no active route has that key")
		return
	}
	if err != nil {
		h.logger.Error("webhook route lookup failed",
			logging.F("route_key", routeKey),
			logging.F("error", err.Error()))
		writeError(w, http.StatusServiceUnavailable, "the route lookup is unavailable")
		return
	}
	if route.Provider != h.normalizer.Provider() ||
		vcs.ProviderKey(route.Provider, route.HostURL, "") != vcs.ProviderKey(h.normalizer.Provider(), h.normalizer.HostURL(), "") {
		h.logger.Warn("webhook route does not match the gateway's provider",
			logging.F("route_key", routeKey),
			logging.F("route_provider", string(route.Provider)),
			logging.F("route_host_url", route.HostURL))
		writeError(w, http.StatusNotFound, "no active route has that key")
		return
	}

	// events.Wrap assigns a fresh event ID. The control plane deduplicates
	// on the provider's delivery ID in the payload, so a redelivery with a
	// new envelope ID still applies once.
	envelopes, err := h.envelopes(r.Context(), event)
	if err != nil {
		h.logger.Error("webhook event could not be encoded", logging.F("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "the webhook event could not be encoded")
		return
	}
	if err := h.publisher.Publish(r.Context(), envelopes...); err != nil {
		h.logger.Error("webhook event could not be published",
			logging.F("delivery_id", deliveryID),
			logging.F("route_key", routeKey),
			logging.F("error", err.Error()))
		writeError(w, http.StatusServiceUnavailable, "the webhook event could not be published")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) envelopes(ctx context.Context, event vcs.WebhookEvent) ([]events.Envelope, error) {
	switch event.Action {
	case vcs.WebhookRepositoryChanged, vcs.WebhookRepositoryArchived, vcs.WebhookRepositoryUnarchived:
		return events.Wrap(ctx, events.Source{}, vcs.ProviderRepositoryChanged{WebhookEvent: event})
	case vcs.WebhookInstallationSuspended, vcs.WebhookInstallationUnsuspended, vcs.WebhookInstallationDeleted:
		return events.Wrap(ctx, events.Source{}, vcs.ProviderInstallationChanged{WebhookEvent: event})
	default:
		return nil, fmt.Errorf("webhook: action %q has no event type", event.Action)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
