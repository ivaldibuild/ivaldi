package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	gatewayconfig "github.com/ivaldibuild/ivaldi-control-plane/internal/gateway/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/gateway/webhook"
	"github.com/ivaldibuild/ivaldi/pkg/env"
	"github.com/ivaldibuild/ivaldi/pkg/events/redisstream"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/redisconn"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// NewGatewayCommand starts the integration gateway.
func NewGatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start the Ivaldi Gateway",
		// Errors here come from configuration or startup, not from how the
		// command was called.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd)
		},
	}
}

func run(cmd *cobra.Command) error {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	var cfg gatewayconfig.Config
	if err := env.Load(&cfg, env.Options{YAMLPath: path}); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger, err := newLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lookupToken, err := cfg.LookupToken()
	if err != nil {
		return err
	}
	webhookSecret, err := cfg.WebhookSecret()
	if err != nil {
		return err
	}
	controlPlaneURL, err := cfg.Gateway.ControlPlaneBaseURL()
	if err != nil {
		return err
	}

	password, err := cfg.Redis.Password()
	if err != nil {
		return err
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
		return fmt.Errorf("redis: %w", err)
	}
	defer func() { _ = redisClient.Close() }()

	publisher, err := redisstream.NewPublisher(redisstream.Config{
		Client: redisClient,
		Stream: cfg.Events.Stream,
	})
	if err != nil {
		return fmt.Errorf("event publisher: %w", err)
	}

	lookup, err := webhook.NewLookupClient(controlPlaneURL, lookupToken, nil)
	if err != nil {
		return err
	}
	resolver, err := webhook.NewResolver(lookup, cfg.Gateway.RouteCacheTTL)
	if err != nil {
		return err
	}
	verifier, err := webhook.NewGitHubSignatureVerifier(webhookSecret)
	if err != nil {
		return err
	}
	normalizer, err := webhook.NewNormalizer(vcs.ProviderGitHub, cfg.VersionControl.GitHub.Host())
	if err != nil {
		return err
	}
	handler, err := webhook.NewHandler(webhook.HandlerConfig{
		Verifier:     verifier,
		Resolver:     resolver,
		Normalizer:   normalizer,
		Publisher:    publisher,
		MaxBodyBytes: cfg.Gateway.MaxBodyBytes,
		Logger:       logger,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("POST "+cfg.Gateway.GitHub.Path, handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// The listener is plain HTTP; a proxy in front of the gateway terminates
	// TLS for providers that require it.
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Gateway.Host, strconv.Itoa(cfg.Gateway.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("gateway listening",
		logging.F("addr", srv.Addr),
		logging.F("stream", cfg.Events.Stream))

	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()

	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("gateway: serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Gateway.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("gateway: shutdown: %w", err)
	}
	return nil
}

// newLogger builds the gateway's logger from the shared logging settings.
func newLogger(c gatewayconfig.Logging) (logging.Logger, error) {
	opts := logging.NewLoggerOptions().
		SetLogLevel(c.Level).
		SetLogOutput(c.Output).
		SetLogFormat(c.Format).
		SetLogFilePrefix(c.FilePrefix).
		SetLogDirectory(c.Directory).
		SetMaxFileSize(c.MaxFileSizeMB).
		SetMaxAge(c.MaxAgeDays)
	return logging.NewLogger(opts)
}
