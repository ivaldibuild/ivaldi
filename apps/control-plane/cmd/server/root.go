package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/webui"
	"github.com/ivaldibuild/ivaldi/pkg/env"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// AGENT_CHART_VERSION is the released ivaldi-agent chart this build was tested
// against, stamped from deploy/helm/ivaldi-agent/version.txt with -ldflags
// "-X github.com/ivaldibuild/ivaldi-control-plane/cmd/server.AGENT_CHART_VERSION".
// The components are released on their own schedules, so the control plane's
// own version names no chart; "0.0.0" means no agent release is pinned yet.
var AGENT_CHART_VERSION = "0.0.0"

// NewServeCommand starts the control plane. version is the binary's.
func NewServeCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Ivaldi Control Plane server",
		// Errors here come from configuration or startup, not from how the
		// command was called, so the usage text would only hide them.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, version)
		},
	}
	// For a local build, which carries no web app; the release image embeds it.
	cmd.Flags().String("web-dir", "", "serve the web app from this directory instead of the copy embedded in the build")
	return cmd
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

	webDir, err := cmd.Flags().GetString("web-dir")
	if err != nil {
		return err
	}
	web, err := webApp(webDir, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, closeDB, err := wire(ctx, &cfg, version, logger)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()

	opts := []api.Option{
		api.WithRoutes(a.services.routes(false)...),
		api.WithHandlers(a.handlers...),
		api.WithVerifier(a.verifier),
	}
	if web != nil {
		opts = append(opts, api.WithWebUI(web))
	}

	srv, err := api.New(&cfg, version, logger, opts...)
	if err != nil {
		return err
	}

	// The relay and the API run until a signal cancels ctx. The relay must
	// stop before the deferred close releases the database and Redis.
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return a.relay.Run(gctx) })
	g.Go(func() error { return a.events.Run(gctx) })
	g.Go(func() error { return a.notificationEvents.Run(gctx) })
	if a.revocationEvents != nil {
		g.Go(func() error { return a.revocationEvents.Run(gctx) })
	}
	g.Go(func() error { return srv.Run(gctx) })
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// webApp is the web app this process serves: the build in dir when one is
// given, otherwise the copy embedded in the binary. A build with neither, such
// as `go run`, serves the API alone and says so: in development the web app
// runs on its own with `ng serve`, proxying to the API.
func webApp(dir string, logger logging.Logger) (http.Handler, error) {
	if dir != "" {
		h, err := webui.Handler(os.DirFS(dir))
		if err != nil {
			return nil, fmt.Errorf("serve the web app from %s: %w", dir, err)
		}
		logger.Info("serving the web app from a directory", logging.F("dir", dir))
		return h, nil
	}
	if assets := webui.Embedded(); assets != nil {
		h, err := webui.Handler(assets)
		if err != nil {
			return nil, fmt.Errorf("serve the embedded web app: %w", err)
		}
		logger.Info("serving the web app embedded in this build")
		return h, nil
	}
	logger.Info("this build carries no web app; the release image does, and `ng serve` serves it in development")
	return nil, nil
}
