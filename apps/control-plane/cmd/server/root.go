package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/env"
)

// NewServeCommand starts the control plane. version is the binary's.
func NewServeCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Ivaldi Control Plane server",
		// Errors here come from configuration or startup, not from how the
		// command was called, so the usage text would only hide them.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, version)
		},
	}
}

func runServe(cmd *cobra.Command, version string) error {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	var cfg config.Config

	if err := env.Load(&cfg, env.Options{YAMLPath: path}); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	logger, err := getLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, closeDB, err := wire(ctx, &cfg, logger)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()

	srv, err := api.New(&cfg, version, logger,
		api.WithRoutes(a.services.routes(false)...),
		api.WithHandlers(a.handlers...),
		api.WithVerifier(a.verifier),
	)
	if err != nil {
		return err
	}

	// The relay and the API run until a signal cancels ctx. The relay must
	// stop before the deferred close releases the database and Redis.
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return a.relay.Run(gctx) })
	g.Go(func() error { return a.events.Run(gctx) })
	g.Go(func() error { return srv.Run(gctx) })
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
