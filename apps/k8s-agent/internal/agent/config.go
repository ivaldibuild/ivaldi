package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/pflag"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/auth"
)

// envPrefix prefixes the environment variable that sets each flag:
// --server-url is IVALDI_SERVER_URL.
const envPrefix = "IVALDI_"

// serviceAccountNamespaceFile names the Pod's namespace when the Kubernetes
// API credentials are mounted.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Config is the agent's configuration. The heartbeat, poll, and watch
// intervals are not here: the control plane sends them.
type Config struct {
	ServerURL            string
	AuthenticationMethod string
	EnrolmentSecret      string
	StateSecret          string
	KubernetesTokenFile  string
	ClientCertFile       string
	ClientKeyFile        string
	ClientCAFile         string
	HealthAddress        string
	LeaseName            string
	// Namespace is the agent's own namespace, holding its Secrets and
	// Lease. Empty means the Pod's namespace.
	Namespace string
	// PodName identifies this replica in the Lease. Empty means the host
	// name, which is the Pod's name.
	PodName string
	// Kubeconfig is for running outside a cluster; empty means the
	// in-cluster configuration.
	Kubeconfig string
	LogLevel   string
	// AllowInsecureHTTP permits an http:// server URL, which sends
	// credentials in plaintext; for local development only.
	AllowInsecureHTTP bool
}

func (c *Config) bind(flags *pflag.FlagSet) {
	flags.StringVar(&c.ServerURL, "server-url", "", "URL of the Ivaldi control plane (required)")
	flags.StringVar(&c.AuthenticationMethod, "authentication-method", string(auth.MethodKubernetes),
		"How the agent authenticates to the control plane: kubernetes, bearer, or mtls")
	flags.StringVar(&c.EnrolmentSecret, "enrolment-secret", "ivaldi-agent-enrolment",
		"Secret in the agent's namespace holding the one-time enrolment token under the key \"token\"")
	flags.StringVar(&c.StateSecret, "state-secret", "ivaldi-agent-state",
		"Secret in the agent's namespace the agent keeps its state in")
	flags.StringVar(&c.KubernetesTokenFile, "kubernetes-token-file", "/var/run/secrets/ivaldi/token",
		"Projected service-account token, for the kubernetes method")
	flags.StringVar(&c.ClientCertFile, "client-cert-file", "", "Client certificate, for the mtls method")
	flags.StringVar(&c.ClientKeyFile, "client-key-file", "", "Client key, for the mtls method")
	flags.StringVar(&c.ClientCAFile, "client-ca-file", "", "CA bundle that verifies the control plane, when it uses a private CA")
	flags.StringVar(&c.HealthAddress, "health-address", ":8080", "Address serving /healthz and /readyz to the kubelet")
	flags.StringVar(&c.LeaseName, "lease-name", "ivaldi-agent", "Lease in the agent's namespace that keeps one replica active")
	flags.StringVar(&c.Namespace, "namespace", "", "The agent's namespace; defaults to the Pod's")
	flags.StringVar(&c.PodName, "pod-name", "", "This replica's name in the Lease; defaults to the host name")
	flags.StringVar(&c.Kubeconfig, "kubeconfig", "", "Kubeconfig for running outside a cluster; defaults to the in-cluster configuration")
	flags.StringVar(&c.LogLevel, "log-level", "info", "Log level: debug, info, warn, or error")
	flags.BoolVar(&c.AllowInsecureHTTP, "allow-insecure-http", false,
		"Allow an http:// server URL, which sends credentials in plaintext; for local development only")
}

// bindEnv sets every flag not given on the command line from its
// environment variable, if set.
func bindEnv(flags *pflag.FlagSet, lookup func(string) (string, bool)) error {
	var errs []error
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			return
		}
		name := envPrefix + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if value, ok := lookup(name); ok {
			if err := flags.Set(f.Name, value); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
		}
	})
	return errors.Join(errs...)
}

// complete fills the defaults that come from the Pod and checks the result.
func (c *Config) complete(readFile func(string) ([]byte, error), hostname func() (string, error)) error {
	var errs []error
	if c.ServerURL == "" {
		errs = append(errs, errors.New("--server-url is required"))
	} else if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		errs = append(errs, fmt.Errorf("--server-url %q must be an https URL", c.ServerURL))
	} else if u.Scheme == "http" && !c.AllowInsecureHTTP {
		errs = append(errs, fmt.Errorf("--server-url %q must use https: the agent sends its credentials to it; pass --allow-insecure-http for local development", c.ServerURL))
	}
	switch method := auth.Method(c.AuthenticationMethod); {
	case !method.Valid():
		errs = append(errs, fmt.Errorf("--authentication-method %q must be kubernetes, bearer, or mtls", c.AuthenticationMethod))
	case method == auth.MethodKubernetes && c.KubernetesTokenFile == "":
		errs = append(errs, errors.New("--kubernetes-token-file is required for the kubernetes method"))
	case method == auth.MethodMTLS && (c.ClientCertFile == "" || c.ClientKeyFile == ""):
		errs = append(errs, errors.New("--client-cert-file and --client-key-file are required for the mtls method"))
	}
	if c.EnrolmentSecret == "" || c.StateSecret == "" || c.LeaseName == "" {
		errs = append(errs, errors.New("--enrolment-secret, --state-secret, and --lease-name must not be empty"))
	}
	if _, err := c.logLevel(); err != nil {
		errs = append(errs, err)
	}
	if c.Namespace == "" {
		raw, err := readFile(serviceAccountNamespaceFile)
		if c.Namespace = strings.TrimSpace(string(raw)); err != nil || c.Namespace == "" {
			errs = append(errs, errors.New("--namespace is required when the Pod's namespace cannot be read"))
		}
	}
	if c.PodName == "" {
		name, err := hostname()
		if err != nil {
			errs = append(errs, fmt.Errorf("--pod-name is required: %w", err))
		}
		c.PodName = name
	}
	return errors.Join(errs...)
}

func (c *Config) logLevel() (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return 0, fmt.Errorf("--log-level %q must be debug, info, warn, or error", c.LogLevel)
	}
	return level, nil
}

func defaultComplete(c *Config) error { return c.complete(os.ReadFile, os.Hostname) }
