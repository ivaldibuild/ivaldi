package iam

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// WorkloadTokenPrefix starts every workload bearer token.
const WorkloadTokenPrefix = "iwt_"

const (
	workloadTokenSecretBytes  = 32
	workloadTokenDisplayChars = 12
)

// IssuedWorkloadToken is a bearer token's secret, display prefix, and hash.
type IssuedWorkloadToken struct {
	Secret string
	Prefix string
	Hash   []byte
}

// NewWorkloadTokenSecret generates a workload bearer token.
func NewWorkloadTokenSecret() (IssuedWorkloadToken, error) {
	b := make([]byte, workloadTokenSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return IssuedWorkloadToken{}, fmt.Errorf("generate workload token: %w", err)
	}
	secret := WorkloadTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return IssuedWorkloadToken{
		Secret: secret,
		Prefix: secret[:workloadTokenDisplayChars],
		Hash:   HashWorkloadToken(secret),
	}, nil
}

// HashWorkloadToken returns the SHA-256 hash stored for a workload token.
func HashWorkloadToken(secret string) []byte { return hashWorkloadSecret(secret) }

// IsWorkloadToken reports whether a token looks like a workload bearer token.
func IsWorkloadToken(token string) bool {
	return strings.HasPrefix(token, WorkloadTokenPrefix)
}
