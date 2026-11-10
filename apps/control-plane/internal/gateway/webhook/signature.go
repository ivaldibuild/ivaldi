package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrInvalidSignature means the provider's signature is missing or does not
// match the body.
var ErrInvalidSignature = errors.New("invalid webhook signature")

// SignatureVerifier verifies a provider's webhook signature.
type SignatureVerifier interface {
	Verify(header string, body []byte) error
}

// GitHubSignatureVerifier verifies GitHub's X-Hub-Signature-256 header.
type GitHubSignatureVerifier struct {
	secret []byte
}

// NewGitHubSignatureVerifier returns a verifier for the App's webhook secret.
func NewGitHubSignatureVerifier(secret string) (*GitHubSignatureVerifier, error) {
	if secret == "" {
		return nil, errors.New("webhook: a GitHub webhook secret is required")
	}
	return &GitHubSignatureVerifier{secret: []byte(secret)}, nil
}

func (v *GitHubSignatureVerifier) Verify(header string, body []byte) error {
	value, ok := strings.CutPrefix(header, "sha256=")
	if !ok || value == "" {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(value)
	if err != nil {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), provided) {
		return ErrInvalidSignature
	}
	return nil
}
