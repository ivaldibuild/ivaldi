package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// tokenRefreshMargin renews an installation token this long before it expires.
const tokenRefreshMargin = time.Minute

type installationToken struct {
	value     string
	expiresAt time.Time
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func parsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("github: the app private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github: parse the app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github: the app private key is not RSA")
	}
	return key, nil
}

// installationToken returns a cached token for the installation, or mints one.
func (c *Client) installationToken(ctx context.Context, base, installationID string) (string, error) {
	key := base + "/" + installationID
	c.mu.Lock()
	token, ok := c.tokens[key]
	c.mu.Unlock()
	if ok && time.Until(token.expiresAt) > tokenRefreshMargin {
		return token.value, nil
	}

	jwt, err := c.jwt()
	if err != nil {
		return "", err
	}
	path := base + "/app/installations/" + url.PathEscape(installationID) + "/access_tokens"
	var body tokenResponse
	if err := c.do(ctx, http.MethodPost, jwt, nil, path, &body); err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(55 * time.Minute)
	if body.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			return "", fmt.Errorf("github: parse the installation token expiry: %w", err)
		}
		expiresAt = parsed
	}
	c.mu.Lock()
	c.tokens[key] = installationToken{value: body.Token, expiresAt: expiresAt}
	c.mu.Unlock()
	return body.Token, nil
}

// jwt signs the short-lived token GitHub accepts for app-level calls.
func (c *Client) jwt() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.appID,
	})
	if err != nil {
		return "", fmt.Errorf("github: encode the app JWT: %w", err)
	}
	payload := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("github: sign the app JWT: %w", err)
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
