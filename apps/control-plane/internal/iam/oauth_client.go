package iam

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// ClientRegistration is how an OAuth client became known.
type ClientRegistration string

const (
	// RegistrationDynamic clients registered themselves through dynamic
	// client registration (RFC 7591).
	RegistrationDynamic ClientRegistration = "dynamic"
	// RegistrationMetadataDocument clients are identified by the https URL
	// of a client metadata document, which is also their client ID.
	RegistrationMetadataDocument ClientRegistration = "metadata_document"
)

// OAuthClient is an MCP or API client of the control plane's authorization
// server. Clients are platform-scoped, because they register before any
// Organization is known; consent and tokens are per Organization.
type OAuthClient struct {
	id           string
	clientID     string
	registration ClientRegistration
	name         string
	redirectURIs []string
	metadataURL  *string
	createdAt    time.Time
	lastUsedAt   *time.Time
}

// OAuthClientSnapshot is an OAuthClient's state, for repositories and audit records.
type OAuthClientSnapshot struct {
	ID           string
	ClientID     string
	Registration ClientRegistration
	Name         string
	RedirectURIs []string
	MetadataURL  *string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// NewOAuthClientProps describes an OAuthClient. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewOAuthClientProps struct {
	// ClientID is the public client_id. It defaults to the client's own ID
	// for a dynamic client, and to MetadataURL for a metadata document client.
	ClientID     string
	Registration ClientRegistration
	Name         string
	RedirectURIs []string
	// MetadataURL is required for a metadata document client, and not
	// allowed for a dynamic one.
	MetadataURL *string
	CreatedAt   *time.Time
	LastUsedAt  *time.Time
}

// NewOAuthClient returns an OAuthClient, with a new ID unless clientID gives a stored one.
func NewOAuthClient(props NewOAuthClientProps, clientID *string) (*OAuthClient, error) {
	if err := validateOwnID(OAuthClient{}.Prefix(), clientID); err != nil {
		return nil, err
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}
	if err := validateRedirectURIs(props.RedirectURIs); err != nil {
		return nil, err
	}
	metadataURL := optionalString(props.MetadataURL)
	switch props.Registration {
	case RegistrationDynamic:
		if metadataURL != nil {
			return nil, NewErrInvalidInput("a dynamically registered client has no metadata URL")
		}
	case RegistrationMetadataDocument:
		if metadataURL == nil {
			return nil, NewErrInvalidInput("a metadata document client needs its metadata URL")
		}
		if err := validateHTTPSURL("metadata URL", *metadataURL); err != nil {
			return nil, err
		}
		if props.ClientID == "" {
			props.ClientID = *metadataURL
		}
		if props.ClientID != *metadataURL {
			return nil, NewErrInvalidInput("a metadata document client's ID must be its metadata URL")
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("client registration %q must be dynamic or metadata_document", props.Registration))
	}
	if len(props.ClientID) > maxClientIDLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("client ID must be at most %d bytes", maxClientIDLength))
	}

	c := &OAuthClient{
		clientID:     props.ClientID,
		registration: props.Registration,
		name:         props.Name,
		redirectURIs: copyStrings(props.RedirectURIs),
		metadataURL:  metadataURL,
		createdAt:    timeOr(props.CreatedAt, clock.Now()),
		lastUsedAt:   clock.CopyTime(props.LastUsedAt),
	}
	if clientID != nil {
		c.id = *clientID
	}
	id.Assign(&c.id, c)
	if c.clientID == "" {
		c.clientID = c.id
	}
	return c, nil
}

// validateRedirectURIs accepts https URIs, http URIs on a loopback address,
// and private-use schemes for native apps, such as com.example.app:/done
// (RFC 8252). None may have a fragment.
func validateRedirectURIs(uris []string) error {
	if len(uris) == 0 || len(uris) > maxRedirectURIs {
		return NewErrInvalidInput(fmt.Sprintf("a client needs 1-%d redirect URIs", maxRedirectURIs))
	}
	for i, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Fragment != "" || strings.Contains(raw, "#") {
			return NewErrInvalidInput(fmt.Sprintf("redirect URI %q must be an absolute URI without a fragment", raw))
		}
		switch {
		case u.Scheme == "https":
			if u.Hostname() == "" {
				return NewErrInvalidInput(fmt.Sprintf("redirect URI %q has no host", raw))
			}
		case u.Scheme == "http":
			if !isLoopback(u.Hostname()) {
				return NewErrInvalidInput(fmt.Sprintf("redirect URI %q must use https unless it is a loopback address", raw))
			}
		case !strings.Contains(u.Scheme, "."):
			return NewErrInvalidInput(fmt.Sprintf("redirect URI %q must use https, loopback http, or a reverse-domain scheme", raw))
		}
		if slices.Contains(uris[:i], raw) {
			return NewErrInvalidInput(fmt.Sprintf("redirect URI %q appears twice", raw))
		}
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (OAuthClient) Prefix() string                      { return "ocl" }
func (c *OAuthClient) ID() string                       { return c.id }
func (c *OAuthClient) ClientID() string                 { return c.clientID }
func (c *OAuthClient) Registration() ClientRegistration { return c.registration }
func (c *OAuthClient) Name() string                     { return c.name }
func (c *OAuthClient) RedirectURIs() []string           { return copyStrings(c.redirectURIs) }
func (c *OAuthClient) MetadataURL() *string             { return copyString(c.metadataURL) }
func (c *OAuthClient) CreatedAt() time.Time             { return c.createdAt }
func (c *OAuthClient) LastUsedAt() *time.Time           { return clock.CopyTime(c.lastUsedAt) }

// AllowsRedirect reports whether uri is one of the client's redirect URIs.
// For a loopback http URI, any port matches, because native apps pick one
// when they start (RFC 8252).
func (c *OAuthClient) AllowsRedirect(uri string) bool {
	if slices.Contains(c.redirectURIs, uri) {
		return true
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return false
	}
	for _, registered := range c.redirectURIs {
		r, err := url.Parse(registered)
		if err == nil && r.Scheme == "http" && r.Hostname() == u.Hostname() &&
			r.Path == u.Path && r.RawQuery == u.RawQuery {
			return true
		}
	}
	return false
}

func (c *OAuthClient) Snapshot() OAuthClientSnapshot {
	return OAuthClientSnapshot{
		ID:           c.id,
		ClientID:     c.clientID,
		Registration: c.registration,
		Name:         c.name,
		RedirectURIs: copyStrings(c.redirectURIs),
		MetadataURL:  copyString(c.metadataURL),
		CreatedAt:    c.createdAt,
		LastUsedAt:   clock.CopyTime(c.lastUsedAt),
	}
}

// Update replaces the client's name and redirect URIs, such as after its
// metadata document changed.
func (c *OAuthClient) Update(name string, redirectURIs []string) error {
	if err := validateName("name", name, maxNameLength); err != nil {
		return err
	}
	if err := validateRedirectURIs(redirectURIs); err != nil {
		return err
	}
	c.name = name
	c.redirectURIs = copyStrings(redirectURIs)
	return nil
}

// RecordUse records that the client was used now.
func (c *OAuthClient) RecordUse() {
	now := clock.Now()
	c.lastUsedAt = &now
}
