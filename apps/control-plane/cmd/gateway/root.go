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
	"github.com/ivaldibuild/ivaldi/pkg/env"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
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

	logger, err := newLogger(cfg.Logging, cfg.Gateway.LogFilePrefix)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	// Each enabled module adds its routes to the shared server. Validate has
	// already refused a configuration with no module enabled.
	if cfg.Gateway.Webhooks.Enabled {
		closeWebhooks, err := mountWebhooks(&cfg, logger, mux)
		if err != nil {
			return fmt.Errorf("webhooks: %w", err)
		}
		defer closeWebhooks()
	}

	// The listener is plain HTTP; a proxy in front of the gateway terminates
	// TLS for providers that require it. The server sets no read or write
	// timeout: modules such as MCP keep responses open for long streams, so
	// each module bounds its own requests (the webhook handler does).
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Gateway.Host, strconv.Itoa(cfg.Gateway.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("gateway listening", logging.F("addr", srv.Addr))

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

// newLogger builds the gateway's logger from the shared logging settings and
// the gateway's own file prefix.
func newLogger(c gatewayconfig.Logging, filePrefix string) (logging.Logger, error) {
	opts := logging.NewLoggerOptions().
		SetLogLevel(c.Level).
		SetLogOutput(c.Output).
		SetLogFormat(c.Format).
		SetLogFilePrefix(filePrefix).
		SetLogDirectory(c.Directory).
		SetMaxFileSize(c.MaxFileSizeMB).
		SetMaxAge(c.MaxAgeDays)
	return logging.NewLogger(opts)
}
