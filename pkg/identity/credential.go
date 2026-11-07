package identity

import (
	"fmt"
	"strings"
	"time"
)

// Internal credential prefixes the API middleware uses to pass non-bearer
// workload credentials to the verifier chain.
const (
	MTLSCredentialPrefix       = "mtls:"
	KubernetesCredentialPrefix = "k8s:"
)

// MTLSCredential encodes a verified client certificate as a credential
// string, with its fingerprint and expiry.
func MTLSCredential(fingerprint string, notAfter time.Time) string {
	return MTLSCredentialPrefix + fingerprint + "|" + notAfter.UTC().Format(time.RFC3339)
}

// ParseMTLSCredential reads a credential built by MTLSCredential.
func ParseMTLSCredential(credential string) (fingerprint string, notAfter time.Time, err error) {
	trimmed := strings.TrimPrefix(credential, MTLSCredentialPrefix)
	i := strings.LastIndex(trimmed, "|")
	if i <= 0 || i == len(trimmed)-1 {
		return "", time.Time{}, fmt.Errorf("%w: malformed mTLS credential", ErrInvalidToken)
	}
	fingerprint = trimmed[:i]
	expiry, parseErr := time.Parse(time.RFC3339, trimmed[i+1:])
	if parseErr != nil {
		return "", time.Time{}, fmt.Errorf("%w: malformed mTLS expiry", ErrInvalidToken)
	}
	return fingerprint, expiry, nil
}

// KubernetesCredential encodes a projected service-account token as a
// credential string.
func KubernetesCredential(token string) string {
	return KubernetesCredentialPrefix + token
}
