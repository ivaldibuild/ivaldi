// Package github reads repositories and commit history from GitHub through a
// GitHub App installation, and connects installations through the App's
// install flow.
//
// The App's private key is the installation's own credential, configured on
// the control plane. A Connection stores the installation ID, and the client
// mints and caches an installation token for it. The client serves the one
// GitHub the App is registered with, github.com or a GitHub Enterprise
// Server, and refuses a Connection that names any other host: the App's JWT
// and its tokens never leave for an address a caller chose.
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

// DefaultWebURL is github.com.
const DefaultWebURL = "https://github.com"

// Config configures the GitHub App client.
type Config struct {
	// AppID is the GitHub App's ID, which signs the JWT that mints
	// installation tokens.
	AppID int64
	// PrivateKeyPEM is the App's private key.
	PrivateKeyPEM []byte
	// Slug is the App's name in its URL, as in https://github.com/apps/<slug>.
	Slug string
	// ClientID and ClientSecret are the App's OAuth credentials, which
	// verify that the person completing an install can access the
	// installation.
	ClientID     string
	ClientSecret string
	// WebURL is the GitHub the App is registered with. Leave it empty for
	// github.com; set it for GitHub Enterprise Server. Connections must name
	// this host.
	WebURL string
	// APIURL overrides the API address, which is otherwise api.github.com
	// for github.com and <WebURL>/api/v3 for GitHub Enterprise Server.
	APIURL string
	// HTTPClient is used for every request. Leave it nil for a default
	// client with a timeout.
	HTTPClient *http.Client
}

// Client reads repositories and commits through GitHub App installations.
type Client struct {
	appID        int64
	key          *rsa.PrivateKey
	slug         string
	clientID     string
	clientSecret string
	webURL       string
	webHost      string
	apiURL       string
	http         *http.Client

	mu     sync.Mutex
	tokens map[string]installationToken
}

var (
	_ vcs.Provider  = (*Client)(nil)
	_ vcs.Installer = (*Client)(nil)
)

// New returns a GitHub client, or an error naming an invalid configuration.
func New(cfg Config) (*Client, error) {
	switch {
	case cfg.AppID <= 0:
		return nil, fmt.Errorf("github: AppID is required")
	case cfg.Slug == "":
		return nil, fmt.Errorf("github: Slug is required")
	case cfg.ClientID == "" || cfg.ClientSecret == "":
		return nil, fmt.Errorf("github: ClientID and ClientSecret are required")
	}
	key, err := parsePrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	webURL := strings.TrimSuffix(cfg.WebURL, "/")
	if webURL == "" {
		webURL = DefaultWebURL
	}
	web, err := url.Parse(webURL)
	if err != nil || web.Host == "" || (web.Scheme != "https" && web.Scheme != "http") {
		return nil, fmt.Errorf("github: WebURL %q must be an absolute http or https URL", cfg.WebURL)
	}
	apiURL := strings.TrimSuffix(cfg.APIURL, "/")
	switch {
	case apiURL != "":
	case strings.EqualFold(web.Host, "github.com"):
		apiURL = "https://api.github.com"
	default:
		apiURL = webURL + "/api/v3"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		appID:        cfg.AppID,
		key:          key,
		slug:         cfg.Slug,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		webURL:       webURL,
		webHost:      strings.ToLower(web.Host),
		apiURL:       apiURL,
		http:         client,
		tokens:       map[string]installationToken{},
	}, nil
}

// checkEndpoint refuses an endpoint this client must not contact: another
// provider, a host other than the configured GitHub, or one without an
// installation.
func (c *Client) checkEndpoint(at vcs.Endpoint) error {
	if at.Provider != vcs.ProviderGitHub {
		return fmt.Errorf("%w: %s", vcs.ErrUnsupportedProvider, at.Provider)
	}
	u, err := url.Parse(at.HostURL)
	if err != nil || !strings.EqualFold(u.Host, c.webHost) {
		return fmt.Errorf("%w: %q is not the configured GitHub host", vcs.ErrUnsupportedProvider, at.HostURL)
	}
	if at.InstallationID == "" {
		return fmt.Errorf("%w: a GitHub connection needs an installation ID", vcs.ErrUnauthorized)
	}
	return nil
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
	case http.StatusForbidden:
		// GitHub answers 403 when a rate limit runs out, not only when it
		// refuses access.
		if res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != "" {
			return vcs.ErrUnavailable
		}
		return vcs.ErrUnauthorized
	case http.StatusUnauthorized:
		return vcs.ErrUnauthorized
	case http.StatusTooManyRequests:
		return vcs.ErrUnavailable
	}
	if res.StatusCode >= 500 {
		return vcs.ErrUnavailable
	}
	return fmt.Errorf("github: the provider answered %s", res.Status)
}

// next returns the rel="next" address in a Link header, or "" on the last
// page. It refuses an address outside the API, so a token is never sent
// anywhere else.
func (c *Client) next(h http.Header) (string, error) {
	link := nextLink(h)
	if link == "" || strings.HasPrefix(link, c.apiURL+"/") {
		return link, nil
	}
	return "", fmt.Errorf("github: the next page %q is outside the API", link)
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
