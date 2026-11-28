// Package agent is the Kubernetes agent's command: its flags, the Lease that
// keeps one replica active, the kubelet's health server, and the run loop
// (docs/design/k8s-agent.md).
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/auth"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy/helm"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/secrets"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/targets"
)

// New returns the agent's root command. version is the build's version.
func New(version string) *cobra.Command {
	var cfg Config
	cmd := &cobra.Command{
		Use:          "ivaldi-k8s-agent",
		Short:        "Ivaldi Kubernetes runtime agent",
		Long:         "The Ivaldi Kubernetes runtime agent deploys Applications to the Targets in its cluster and reports what runs there.",
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bindEnv(cmd.Flags(), os.LookupEnv); err != nil {
				return err
			}
			if err := defaultComplete(&cfg); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return start(ctx, cfg, version)
		},
	}
	cfg.bind(cmd.Flags())
	return cmd
}

func start(ctx context.Context, cfg Config, version string) error {
	level, _ := cfg.logLevel()
	// Every log line passes through the masker, so no secret value the
	// agent resolves can reach its logs.
	masker := secrets.NewMasker()
	log := slog.New(secrets.NewMaskingHandler(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}), masker))
	log.Info("starting", "version", version, "namespace", cfg.Namespace, "server_url", cfg.ServerURL,
		"authentication_method", cfg.AuthenticationMethod)

	restConfig, err := kubeConfig(cfg.Kubeconfig)
	if err != nil {
		return err
	}
	restConfig.UserAgent = "ivaldi-k8s-agent/" + version
	kube, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	creds, err := auth.New(auth.Config{
		ServerURL:           cfg.ServerURL,
		Method:              auth.Method(cfg.AuthenticationMethod),
		KubernetesTokenFile: cfg.KubernetesTokenFile,
		ClientCertFile:      cfg.ClientCertFile,
		ClientKeyFile:       cfg.ClientKeyFile,
		ClientCAFile:        cfg.ClientCAFile,
		State:               state.NewSecretStore(kube, cfg.Namespace, cfg.StateSecret),
		Enrolment:           state.NewEnrolmentSecret(kube, cfg.Namespace, cfg.EnrolmentSecret),
		Logger:              log,
	})
	if err != nil {
		return err
	}

	cp, err := apiclient.NewControlPlane(cfg.ServerURL, creds.Client())
	if err != nil {
		return err
	}
	helmDeployer, err := helm.New(helm.Config{
		RESTConfig:          restConfig,
		Kube:                kube,
		AgentNamespace:      cfg.Namespace,
		PullSecret:          cfg.RegistryPullSecret,
		PlainHTTPRegistries: cfg.PlainHTTPRegistries,
		Timeout:             applyTimeout,
		Logger:              log,
	})
	if err != nil {
		return err
	}
	secretsCfg := secrets.Config{
		Kube: kube, AgentNamespace: cfg.Namespace, FingerprintSecret: cfg.FingerprintSecret,
		Addresses: cfg.SecretAddresses, VaultTokenFile: cfg.VaultTokenFile, InfisicalTokenFile: cfg.InfisicalTokenFile, Credentials: cfg.Credentials,
		Grants: cp.SecretGrants, Masker: masker, Logger: log,
	}
	a := &agent{
		cfg:        cfg,
		version:    version,
		log:        log,
		kube:       kube,
		creds:      creds,
		cp:         cp,
		newTracker: func() targets.Tracker { return targets.NewTracker(cp, kube, log) },
		deployers:  map[string]deploy.Deployer{helmDeployer.Name(): helmDeployer},
		reporter:   report.New(cp, log),
		secrets:    secrets.NewResolver(secretsCfg),
		secretCapabilities: targets.SecretCapabilities{
			StoreKinds: secretsCfg.StoreKinds(), StoreModes: secretsCfg.StoreModes(), Credentials: secretsCfg.CredentialNames(),
		},
		secretWriter: secrets.NewWriter(kube),
		masker:       masker,
		ready:        newReadiness(conditionEnrolled, conditionLease, conditionTargets),
		enrolRetry:   enrolRetry,
		retryBase:    time.Second,
	}
	group, ctx := errgroup.WithContext(ctx)
	runCtx, stopHealth := context.WithCancel(ctx)
	group.Go(func() error { return serveHealth(runCtx, cfg.HealthAddress, a.ready) })
	group.Go(func() error {
		defer stopHealth()
		return a.run(ctx)
	})
	err = group.Wait()
	log.Info("stopped", "error", err)
	return err
}

func kubeConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		c, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		return c, nil
	}
	c, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster kubernetes configuration (use --kubeconfig outside a cluster): %w", err)
	}
	return c, nil
}
