package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// The control plane reads a bearer token that is not an Ivaldi token by this
// header (pkg/identity.CredentialHeader); the agent keeps its own copy rather
// than import pkg/identity's JOSE dependencies.
const (
	credentialHeader           = "X-Ivaldi-Credential"
	credentialHeaderKubernetes = "kubernetes"
)

// requestTimeout bounds one call to the control plane.
const requestTimeout = 30 * time.Second

// baseTransport is the TLS transport every request uses: the private CA when
// one is configured, and the client certificate for mtls, read from disk at
// each handshake so a rotated certificate is picked up without a restart.
func baseTransport(cfg Config) (*http.Transport, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.ClientCAFile != "" {
		pem, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("client CA file %s holds no PEM certificate", cfg.ClientCAFile)
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.Method == MethodMTLS {
		if _, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile); err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
			if err != nil {
				return nil, fmt.Errorf("load client certificate: %w", err)
			}
			return &cert, nil
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return transport, nil
}

// authTransport adds the agent's credential to every request, and tells the
// manager when the control plane no longer accepts it.
type authTransport struct {
	next    http.RoundTripper
	manager *manager
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if err := t.manager.authenticate(req); err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.manager.Invalidate(fmt.Sprintf("the control plane refused the credential on %s %s", req.Method, req.URL.Path))
	}
	return resp, nil
}

// readTokenFile returns the projected token, re-read on every request
// because the kubelet rotates it.
func readTokenFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read kubernetes token file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", errors.New("the kubernetes token file is empty")
	}
	return token, nil
}
