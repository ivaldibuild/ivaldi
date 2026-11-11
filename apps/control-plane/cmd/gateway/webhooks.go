package gateway

import (
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"

	gatewayconfig "github.com/ivaldibuild/ivaldi-control-plane/internal/gateway/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/gateway/webhook"
	"github.com/ivaldibuild/ivaldi/pkg/events/redisstream"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/redisconn"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// mountWebhooks adds the provider webhook module to mux. It returns a
// function that releases what the module opened, such as its Redis client.
func mountWebhooks(cfg *gatewayconfig.Config, logger logging.Logger, mux *http.ServeMux) (func(), error) {
	signer, err := cfg.Signer()
	if err != nil {
		return nil, err
	}
	webhookSecret, err := cfg.WebhookSecret()
	if err != nil {
		return nil, err
	}
	controlPlaneURL, err := cfg.Gateway.ControlPlaneBaseURL()
	if err != nil {
		return nil, err
	}

	password, err := cfg.Redis.Password()
	if err != nil {
		return nil, err
	}
	redisClient, err := redisconn.New(redisconn.Config{
		Address:     cfg.Redis.Address,
		Username:    cfg.Redis.Username,
		Password:    password,
		Database:    cfg.Redis.Database,
		TLS:         cfg.Redis.TLS,
		CAFile:      cfg.Redis.CAFile,
		DialTimeout: cfg.Redis.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}
	closeRedis := func() { _ = redisClient.Close() }

	handler, err := newWebhookHandler(cfg, logger, redisClient, controlPlaneURL, signer, webhookSecret)
	if err != nil {
		closeRedis()
		return nil, err
	}
	mux.Handle("POST "+cfg.Gateway.Webhooks.GitHub.Path, handler)
	logger.Info("webhooks enabled",
		logging.F("github_path", cfg.Gateway.Webhooks.GitHub.Path),
		logging.F("stream", cfg.Events.Stream))
	return closeRedis, nil
}

func newWebhookHandler(cfg *gatewayconfig.Config, logger logging.Logger, redisClient redis.UniversalClient, controlPlaneURL string, signer identity.WorkloadKeySigner, webhookSecret string) (http.Handler, error) {
	publisher, err := redisstream.NewPublisher(redisstream.Config{
		Client: redisClient,
		Stream: cfg.Events.Stream,
	})
	if err != nil {
		return nil, fmt.Errorf("event publisher: %w", err)
	}
	lookup, err := webhook.NewLookupClient(controlPlaneURL, signer, nil)
	if err != nil {
		return nil, err
	}
	resolver, err := webhook.NewResolver(lookup, cfg.Gateway.Webhooks.RouteCacheTTL)
	if err != nil {
		return nil, err
	}
	verifier, err := webhook.NewGitHubSignatureVerifier(webhookSecret)
	if err != nil {
		return nil, err
	}
	normalizer, err := webhook.NewNormalizer(vcs.ProviderGitHub, cfg.VersionControl.GitHub.Host())
	if err != nil {
		return nil, err
	}
	return webhook.NewHandler(webhook.HandlerConfig{
		Verifier:     verifier,
		Resolver:     resolver,
		Normalizer:   normalizer,
		Publisher:    publisher,
		MaxBodyBytes: cfg.Gateway.Webhooks.MaxBodyBytes,
		Timeout:      cfg.Gateway.Webhooks.RequestTimeout,
		Logger:       logger,
	})
}
