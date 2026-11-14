package helm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/values"
)

// fetch pulls the chart by digest, with the pull Secret's credentials for
// its registry when there are any.
func (d *deployer) fetch(ctx context.Context, artifact deploy.Artifact) (*chart.Chart, error) {
	options := []registry.ClientOption{
		// Never read or write credentials on disk: the root filesystem is
		// read-only, and credentials come from the pull Secret.
		registry.ClientOptCredentialsFile(filepath.Join(os.TempDir(), "ivaldi-helm-registry.json")),
		// Helm pulls with context.Background; the bound transport makes a
		// stalled registry give up when the apply does.
		registry.ClientOptHTTPClient(&http.Client{Transport: bind(ctx, registry.NewTransport(false))}),
	}
	username, password, err := d.registryCredentials(ctx, registryHost(artifact.Repository))
	if err != nil {
		return nil, err
	}
	if username != "" {
		options = append(options, registry.ClientOptBasicAuth(username, password))
	}
	client, err := registry.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("registry client: %w", err)
	}
	pulled, err := client.Pull(artifact.Ref())
	if err != nil {
		return nil, &deploy.Failure{Reason: fmt.Sprintf("could not pull chart %s", artifact.Ref()), Err: err}
	}
	loaded, err := loader.LoadArchive(bytes.NewReader(pulled.Chart.Data))
	if err != nil {
		return nil, &deploy.Failure{Reason: fmt.Sprintf("chart %s is not a valid chart", artifact.Ref()), Err: err}
	}
	return loaded, nil
}

// registryCredentials reads the docker-config pull Secret in the agent's
// namespace and returns the credentials for host, if it has any.
func (d *deployer) registryCredentials(ctx context.Context, host string) (string, string, error) {
	if d.cfg.PullSecret == "" {
		return "", "", nil
	}
	secret, err := d.cfg.Kube.CoreV1().Secrets(d.cfg.AgentNamespace).Get(ctx, d.cfg.PullSecret, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("read registry pull secret %s/%s: %w", d.cfg.AgentNamespace, d.cfg.PullSecret, err)
	}
	raw := secret.Data[corev1.DockerConfigJsonKey]
	if len(raw) == 0 {
		return "", "", fmt.Errorf("registry pull secret %s has no %s", d.cfg.PullSecret, corev1.DockerConfigJsonKey)
	}
	return credentialsFor(raw, host)
}

// credentialsFor returns host's credentials from a .dockerconfigjson.
func credentialsFor(raw []byte, host string) (string, string, error) {
	var config struct {
		Auths map[string]struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Auth     string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", "", fmt.Errorf("registry pull secret is not a docker config: %w", err)
	}
	for key, entry := range config.Auths {
		if registryHost(strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")) != host {
			continue
		}
		if entry.Username != "" {
			return entry.Username, entry.Password, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return "", "", fmt.Errorf("registry pull secret: auth for %s is not base64", key)
		}
		username, password, _ := strings.Cut(string(decoded), ":")
		return username, password, nil
	}
	return "", "", nil
}

// registryHost is the registry part of a repository, such as ghcr.io.
func registryHost(repository string) string {
	repository = strings.TrimPrefix(repository, "oci://")
	host, _, _ := strings.Cut(repository, "/")
	return host
}

// overlays returns the selected overlay files from the chart, in order.
func overlays(c *chart.Chart, names []string) ([]values.Document, error) {
	out := make([]values.Document, 0, len(names))
	for _, name := range names {
		var found []byte
		for _, f := range c.Raw {
			if f.Name == name {
				found = f.Data
				break
			}
		}
		if found == nil {
			return nil, deploy.Failf("overlay %s is not in the chart", name)
		}
		out = append(out, values.Document{Name: name, Content: found})
	}
	return out, nil
}
