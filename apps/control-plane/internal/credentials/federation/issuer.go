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

// Audience is a store's audience: fixed and unique to it, so a store that
// checks only the audience still refuses a JWT made for another tenant's.
func Audience(storeID string) string { return "ivaldi:" + storeID }

// Subject is what a store that matches only the subject separates
// Environments by. It uses IDs, so renaming never makes a JWT match another
// policy.
func Subject(organizationID, storeID, environmentID string) string {
	return fmt.Sprintf("org:%s:store:%s:env:%s", organizationID, storeID, environmentID)
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
