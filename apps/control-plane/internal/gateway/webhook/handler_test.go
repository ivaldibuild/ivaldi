package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type fakeVerifier struct {
	err error
}

func (f fakeVerifier) Verify(string, []byte) error { return f.err }

type fakeResolver struct {
	route vcs.WebhookRoute
	err   error
	calls int
}

func (f *fakeResolver) Resolve(context.Context, string) (vcs.WebhookRoute, error) {
	f.calls++
	return f.route, f.err
}

type fakePublisher struct {
	envelopes []events.Envelope
	err       error
	calls     int
}

func (f *fakePublisher) Publish(_ context.Context, envelopes ...events.Envelope) error {
	f.calls++
	f.envelopes = append(f.envelopes, envelopes...)
	return f.err
}

type handlerFixture struct {
	handler   *Handler
	resolver  *fakeResolver
	publisher *fakePublisher
}

func newTestHandler(t *testing.T, options ...func(*HandlerConfig)) *handlerFixture {
	t.Helper()
	normalizer, err := NewNormalizer(vcs.ProviderGitHub, "https://github.com")
	require.NoError(t, err)
	cfg := HandlerConfig{
		Verifier:     fakeVerifier{},
		Resolver:     &fakeResolver{route: testRoute},
		Normalizer:   normalizer,
		Publisher:    &fakePublisher{},
		MaxBodyBytes: 1 << 20,
		Logger:       logging.Nop(),
	}
	for _, option := range options {
		option(&cfg)
	}
	handler, err := NewHandler(cfg)
	require.NoError(t, err)
	return &handlerFixture{
		handler:   handler,
		resolver:  cfg.Resolver.(*fakeResolver),
		publisher: cfg.Publisher.(*fakePublisher),
	}
}

func webhookRequest(event, delivery, signature string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set(headerEvent, event)
	req.Header.Set(headerDelivery, delivery)
	req.Header.Set(headerSignature, signature)
	return req
}

const repositoryBody = `{
  "action": "renamed",
  "installation": {"id": 42, "updated_at": "2026-11-09T10:00:00Z"},
  "repository": {
    "id": 99,
    "full_name": "acme/checkout",
    "default_branch": "main",
    "visibility": "private",
    "clone_url": "https://github.com/acme/checkout.git",
    "html_url": "https://github.com/acme/checkout",
    "archived": false,
    "updated_at": "2026-11-09T09:00:00Z"
  }
}`

func TestHandlerPublishesARepositoryEvent(t *testing.T) {
	f := newTestHandler(t)
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, 1, f.publisher.calls)
	require.Len(t, f.publisher.envelopes, 1)
	envelope := f.publisher.envelopes[0]
	assert.Equal(t, vcs.EventProviderRepositoryChanged, envelope.Type)

	var event vcs.ProviderRepositoryChanged
	require.NoError(t, json.Unmarshal(envelope.Payload, &event))
	assert.Equal(t, vcs.WebhookRepositoryChanged, event.Action)
	assert.Equal(t, "delivery-1", event.DeliveryID)
	assert.Equal(t, "github:github.com:42", event.RouteKey)
	require.NotNil(t, event.Repository)
	assert.Equal(t, "99", event.Repository.ExternalID)
	assert.Equal(t, "acme/checkout", event.Repository.FullName)
	assert.Equal(t, time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), event.OccurredAt)
}

func TestHandlerPublishesAnInstallationEvent(t *testing.T) {
	f := newTestHandler(t)
	body := []byte(`{"action":"suspend","installation":{"id":42,"updated_at":"2026-11-09T10:00:00Z"}}`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventInstallation, "delivery-1", sign(t, testSecret, body), body))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, f.publisher.envelopes, 1)
	assert.Equal(t, vcs.EventProviderInstallationChanged, f.publisher.envelopes[0].Type)
	var event vcs.ProviderInstallationChanged
	require.NoError(t, json.Unmarshal(f.publisher.envelopes[0].Payload, &event))
	assert.Equal(t, vcs.WebhookInstallationSuspended, event.Action)
	assert.Equal(t, "42", event.InstallationID)
}

func TestHandlerAcknowledgesPingWithoutARoute(t *testing.T) {
	f := newTestHandler(t)
	body := []byte(`{"zen":"hello"}`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest("ping", "delivery-1", sign(t, testSecret, body), body))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Zero(t, f.resolver.calls)
	assert.Zero(t, f.publisher.calls)
}

func TestHandlerAcknowledgesAnUnhandledActionWithoutARoute(t *testing.T) {
	// installation.created arrives before its Connection exists, so looking
	// up its route would fail the delivery.
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Resolver = &fakeResolver{err: ErrRouteNotFound}
	})
	body := []byte(`{"action":"created","installation":{"id":42}}`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventInstallation, "delivery-1", sign(t, testSecret, body), body))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Zero(t, f.resolver.calls)
	assert.Zero(t, f.publisher.calls)
}

func TestHandlerRejectsABadSignatureBeforeParsing(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Verifier = fakeVerifier{err: ErrInvalidSignature}
	})
	body := []byte(`{not json`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", "sha256=bad", body))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Zero(t, f.resolver.calls)
	assert.Zero(t, f.publisher.calls)
}

func TestHandlerRejectsMalformedJSON(t *testing.T) {
	f := newTestHandler(t)
	body := []byte(`{not json`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Zero(t, f.publisher.calls)
}

func TestHandlerRejectsAnOversizedBody(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.MaxBodyBytes = 10
	})
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestHandlerRejectsAMissingInstallation(t *testing.T) {
	f := newTestHandler(t)
	body := []byte(`{"action":"renamed","repository":{"id":99,"full_name":"acme/api","visibility":"private"}}`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Zero(t, f.resolver.calls)
}

func TestHandlerReturnsNotFoundForAnUnknownRoute(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Resolver = &fakeResolver{err: ErrRouteNotFound}
	})
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Zero(t, f.publisher.calls)
}

func TestHandlerReturnsUnavailableWhenTheLookupFails(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Resolver = &fakeResolver{err: ErrLookupUnavailable}
	})
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlerReturnsUnavailableWhenPublishingFails(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Publisher = &fakePublisher{err: errors.New("redis down")}
	})
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlerRejectsAProviderMismatch(t *testing.T) {
	f := newTestHandler(t, func(c *HandlerConfig) {
		c.Resolver = &fakeResolver{route: vcs.WebhookRoute{
			Provider: vcs.ProviderGitLab, HostURL: "https://gitlab.com",
		}}
	})
	body := []byte(repositoryBody)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, webhookRequest(eventRepository, "delivery-1", sign(t, testSecret, body), body))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Zero(t, f.publisher.calls)
}

func TestNewHandlerValidatesItsInput(t *testing.T) {
	normalizer, err := NewNormalizer(vcs.ProviderGitHub, "https://github.com")
	require.NoError(t, err)
	_, err = NewHandler(HandlerConfig{})
	assert.Error(t, err)
	_, err = NewHandler(HandlerConfig{
		Verifier: fakeVerifier{}, Resolver: &fakeResolver{}, Normalizer: normalizer,
		Publisher: &fakePublisher{}, MaxBodyBytes: 1, Logger: logging.Nop(),
	})
	assert.NoError(t, err)
}
