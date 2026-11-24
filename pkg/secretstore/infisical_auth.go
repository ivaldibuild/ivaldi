package secretstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// InfisicalMethod is a machine identity's auth method that logs in with a
// JWT.
type InfisicalMethod string

const (
	// InfisicalOIDC checks the JWT against its issuer's discovery document.
	InfisicalOIDC InfisicalMethod = "oidc"
	// InfisicalJWT checks the JWT against keys configured on the identity,
	// for an issuer Infisical cannot reach.
	InfisicalJWT InfisicalMethod = "jwt"
	// InfisicalKubernetes checks a service-account token with the
	// cluster's TokenReview API.
	InfisicalKubernetes InfisicalMethod = "kubernetes"
)

// InfisicalLogin logs in as a machine identity with a JWT, through the
// identity's OIDC, JWT, or Kubernetes auth, and returns an access token.
// siteURL is Infisical's address, or empty for DefaultInfisicalSite, and
// receives the JWT; client may be nil.
func InfisicalLogin(ctx context.Context, client *http.Client, siteURL string, method InfisicalMethod, identityID, jwt string) (string, error) {
	switch method {
	case InfisicalOIDC, InfisicalJWT, InfisicalKubernetes:
	default:
		return "", fmt.Errorf("%w: Infisical has no %q login", ErrDenied, method)
	}
	return infisicalLogin(ctx, client, siteURL, string(method)+"-auth", map[string]string{"identityId": identityID, "jwt": jwt})
}

// InfisicalUniversalAuthLogin logs in as a machine identity with its
// Universal Auth client ID and secret, and returns an access token.
func InfisicalUniversalAuthLogin(ctx context.Context, client *http.Client, siteURL, clientID, clientSecret string) (string, error) {
	return infisicalLogin(ctx, client, siteURL, "universal-auth", map[string]string{"clientId": clientID, "clientSecret": clientSecret})
}

func infisicalLogin(ctx context.Context, client *http.Client, siteURL, auth string, payload map[string]string) (string, error) {
	if siteURL == "" {
		siteURL = DefaultInfisicalSite
	}
	if client == nil {
		client = noRedirects()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(siteURL, "/") + "/api/v1/auth/" + auth + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: infisical %s login: %v", ErrUnavailable, auth, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", fmt.Errorf("%w: infisical %s login answered %d", ErrUnavailable, auth, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		// The identity refused the login: an unknown identity, or a JWT or
		// client secret its auth method does not accept.
		return "", fmt.Errorf("%w: infisical %s login answered %d", ErrDenied, auth, resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("%w: infisical %s login gave no access token", ErrUnavailable, auth)
	}
	return out.AccessToken, nil
}
