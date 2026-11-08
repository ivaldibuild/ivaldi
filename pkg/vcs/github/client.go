// Package github reads repositories and commit history from GitHub through a
// GitHub App installation.
//
// The App's private key is the installation's own credential, configured on
// the control plane. A Connection stores the installation ID, and the client
// mints and caches an installation token for it. Web URLs, such as
// https://github.com, and API URLs differ: github.com uses api.github.com,
// and GitHub Enterprise Server uses <host>/api/v3.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// apiVersion is the GitHub REST API version the client asks for.
const apiVersion = "2022-11-28"

// Config configures the GitHub App client.
type Config struct {
	// AppID is the GitHub App's ID, which signs the JWT that mints
	// installation tokens.
	AppID int64
	// PrivateKeyPEM is the App's private key.
	PrivateKeyPEM []byte
	// BaseURL overrides the API address. Leave it empty to derive it from
	// each connection's host URL.
	BaseURL string
	// HTTPClient is used for every request. Leave it nil for a default
	// client with a timeout.
	HTTPClient *http.Client
}

// Client reads repositories and commits through GitHub App installations.
type Client struct {
	appID   int64
	key     *rsa.PrivateKey
	baseURL string
	http    *http.Client

	mu     sync.Mutex
	tokens map[string]installationToken
}

var _ vcs.Provider = (*Client)(nil)

// New returns a GitHub client, or an error naming an invalid configuration.
func New(cfg Config) (*Client, error) {
	if cfg.AppID <= 0 {
		return nil, fmt.Errorf("github: AppID is required")
	}
	key, err := parsePrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		appID:   cfg.AppID,
		key:     key,
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		http:    client,
		tokens:  map[string]installationToken{},
	}, nil
}

// base returns the API address for a connection's provider.
func (c *Client) base(at vcs.Endpoint) (string, error) {
	if at.Provider != vcs.ProviderGitHub {
		return "", fmt.Errorf("%w: %s", vcs.ErrUnsupportedProvider, at.Provider)
	}
	if at.InstallationID == "" {
		return "", fmt.Errorf("%w: a GitHub connection needs an installation ID", vcs.ErrUnauthorized)
	}
	if c.baseURL != "" {
		return c.baseURL, nil
	}
	u, err := url.Parse(at.HostURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("github: invalid host URL %q", at.HostURL)
	}
	if u.Host == "github.com" {
		return "https://api.github.com", nil
	}
	return u.Scheme + "://" + u.Host + "/api/v3", nil
}

func (c *Client) doRequest(ctx context.Context, method, token string, body []byte, path string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", vcs.ErrUnavailable, err)
	}
	return res, nil
}

// do sends a request and decodes its response into out.
func (c *Client) do(ctx context.Context, method, token string, body []byte, path string, out any) error {
	res, err := c.doRequest(ctx, method, token, body, path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return decodeResponse(res, out)
}

// get sends a GET and decodes its response into out.
func (c *Client) get(ctx context.Context, token, path string, out any) error {
	_, err := c.getWithHeader(ctx, token, path, out)
	return err
}

func (c *Client) getWithHeader(ctx context.Context, token, path string, out any) (http.Header, error) {
	res, err := c.doRequest(ctx, http.MethodGet, token, nil, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := decodeResponse(res, out); err != nil {
		return nil, err
	}
	return res.Header, nil
}

func decodeResponse(res *http.Response, out any) error {
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return statusError(res)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: read the response: %w", vcs.ErrUnavailable, err)
	}
	return nil
}

// statusError maps GitHub's refusals to the errors callers handle.
func statusError(res *http.Response) error {
	switch res.StatusCode {
	case http.StatusNotFound:
		return vcs.ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return vcs.ErrUnauthorized
	case http.StatusTooManyRequests:
		return vcs.ErrUnavailable
	}
	if res.StatusCode >= 500 {
		return vcs.ErrUnavailable
	}
	return fmt.Errorf("github: the provider answered %s", res.Status)
}

// nextLink returns the rel="next" address in a Link header, or "".
func nextLink(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if len(fields) < 2 {
			continue
		}
		if slices.ContainsFunc(fields[1:], func(f string) bool {
			return strings.TrimSpace(f) == `rel="next"`
		}) {
			return strings.Trim(strings.TrimSpace(fields[0]), "<>")
		}
	}
	return ""
}
