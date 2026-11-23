// Package federation makes the control plane an OIDC issuer for Secret
// Stores (ADR 0017). For each grant it signs a short-lived JWT naming the
// Deployment it is for, which the agent exchanges at a store whose own
// configuration trusts the issuer. It is not the API's token issuer: it has
// its own path, keys, and audiences, so a store that trusts it accepts no
// access token, and the API accepts none of its JWTs.
package federation

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const (
	// Path is the issuer's path under the control plane's external URL.
	Path = "/federation"
	// DiscoveryPath is where OIDC Discovery looks for an issuer with a path:
	// the issuer, then /.well-known/openid-configuration.
	DiscoveryPath = Path + "/.well-known/openid-configuration"
	// JWKSPath publishes the keys that verify the JWTs.
	JWKSPath = Path + "/jwks.json"
	// Lifetime is how long a JWT may be exchanged: long enough for an agent
	// to reach its store, short enough that one that leaks is soon useless.
	Lifetime = 5 * time.Minute
	// tokenType is the JWTs' typ header.
	tokenType = "JWT"
)

// Audience is a store's audience: fixed and unique to it, so a store that
// checks only the audience still refuses a JWT made for another tenant's.
func Audience(storeID string) string { return "ivaldi:" + storeID }

// Subject is what a store that matches only the subject separates
// Environments by. It uses IDs, so renaming never makes a JWT match another
// policy.
func Subject(organizationID, storeID, environmentID string) string {
	return fmt.Sprintf("org:%s:store:%s:env:%s", organizationID, storeID, environmentID)
}

// Grant is what one JWT is for: one store, read for one Deployment.
type Grant struct {
	OrganizationID  string
	WorkspaceID     string
	ProjectID       string
	ApplicationID   string
	EnvironmentID   string
	EnvironmentTier string
	TargetID        string
	ClusterID       string
	DeploymentID    string
	ReleaseID       string
	StoreID         string
}

// Token is a signed JWT. Its value is a credential: it is sent to the agent
// and never stored, logged, or put in a record.
type Token struct {
	Value     string
	ID        string
	ExpiresAt time.Time
}

// Claims are a JWT's claims: the registered ones, and the IDs a store with
// bound claims can match on.
type Claims struct {
	Issuer          string   `json:"iss"`
	Subject         string   `json:"sub"`
	Audience        []string `json:"aud"`
	IssuedAt        int64    `json:"iat"`
	NotBefore       int64    `json:"nbf"`
	ExpiresAt       int64    `json:"exp"`
	ID              string   `json:"jti"`
	Organization    string   `json:"org"`
	Workspace       string   `json:"workspace"`
	Project         string   `json:"project"`
	Application     string   `json:"application"`
	Environment     string   `json:"environment"`
	EnvironmentTier string   `json:"environment_tier"`
	Target          string   `json:"target"`
	Cluster         string   `json:"cluster"`
	Deployment      string   `json:"deployment"`
	Release         string   `json:"release"`
}

// Issuer signs the JWTs grants carry.
type Issuer interface {
	// URL is the issuer: the control plane's external URL and Path.
	URL() string
	Issue(g Grant) (Token, error)
}

type issuer struct {
	keys token.KeyRing
	url  string
}

// NewIssuer returns the issuer at externalURL + Path, signing with keys,
// which must not be the API's.
func NewIssuer(keys token.KeyRing, externalURL string) (Issuer, error) {
	if keys == nil {
		return nil, errors.New("federation: the issuer needs signing keys")
	}
	iss, err := IssuerURL(externalURL)
	if err != nil {
		return nil, err
	}
	return &issuer{keys: keys, url: iss}, nil
}

// IssuerURL is the issuer for the control plane at externalURL.
func IssuerURL(externalURL string) (string, error) {
	u, err := url.Parse(externalURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("federation: the external URL %q is not an absolute URL", externalURL)
	}
	return strings.TrimRight(externalURL, "/") + Path, nil
}

func (i *issuer) URL() string { return i.url }

func (i *issuer) Issue(g Grant) (Token, error) {
	switch {
	case g.OrganizationID == "", g.StoreID == "", g.EnvironmentID == "", g.DeploymentID == "":
		return Token{}, errors.New("federation: a grant names its organization, store, environment, and deployment")
	}
	now := clock.Now().Truncate(time.Second)
	expires := now.Add(Lifetime)
	c := Claims{
		Issuer:          i.url,
		Subject:         Subject(g.OrganizationID, g.StoreID, g.EnvironmentID),
		Audience:        []string{Audience(g.StoreID)},
		IssuedAt:        now.Unix(),
		NotBefore:       now.Unix(),
		ExpiresAt:       expires.Unix(),
		ID:              id.ULID(),
		Organization:    g.OrganizationID,
		Workspace:       g.WorkspaceID,
		Project:         g.ProjectID,
		Application:     g.ApplicationID,
		Environment:     g.EnvironmentID,
		EnvironmentTier: g.EnvironmentTier,
		Target:          g.TargetID,
		Cluster:         g.ClusterID,
		Deployment:      g.DeploymentID,
		Release:         g.ReleaseID,
	}
	value, err := i.keys.SignJWT(tokenType, c)
	if err != nil {
		return Token{}, fmt.Errorf("federation: %w", err)
	}
	return Token{Value: value, ID: c.ID, ExpiresAt: expires}, nil
}
