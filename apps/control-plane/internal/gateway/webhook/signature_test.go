package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "webhook-secret"

func sign(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, err := mac.Write(body)
	require.NoError(t, err)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubSignatureVerifierAcceptsAValidSignature(t *testing.T) {
	verifier, err := NewGitHubSignatureVerifier(testSecret)
	require.NoError(t, err)
	body := []byte(`{"action":"renamed"}`)

	assert.NoError(t, verifier.Verify(sign(t, testSecret, body), body))
}

func TestGitHubSignatureVerifierRejectsBadSignatures(t *testing.T) {
	verifier, err := NewGitHubSignatureVerifier(testSecret)
	require.NoError(t, err)
	body := []byte(`{"action":"renamed"}`)

	tests := map[string]string{
		"missing":       "",
		"wrong prefix":  "sha1=abc",
		"malformed hex": "sha256=not-hex",
		"wrong secret":  sign(t, "another-secret", body),
		"wrong body":    sign(t, testSecret, []byte(`{"action":"edited"}`)),
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, verifier.Verify(header, body), ErrInvalidSignature)
		})
	}
}

func TestNewGitHubSignatureVerifierRequiresASecret(t *testing.T) {
	_, err := NewGitHubSignatureVerifier("")
	assert.Error(t, err)
}
