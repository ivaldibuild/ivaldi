package iam

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// WorkloadEnrollmentTokenPrefix starts every workload enrolment token.
const WorkloadEnrollmentTokenPrefix = "iwe_"

const workloadEnrollmentTokenSecretBytes = 32

// IssuedWorkloadEnrollmentToken is an enrolment token's secret and hash.
type IssuedWorkloadEnrollmentToken struct {
	Secret string
	Hash   []byte
}

// NewWorkloadEnrollmentTokenSecret generates a single-use enrolment token.
func NewWorkloadEnrollmentTokenSecret() (IssuedWorkloadEnrollmentToken, error) {
	b := make([]byte, workloadEnrollmentTokenSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return IssuedWorkloadEnrollmentToken{}, fmt.Errorf("generate workload enrollment token: %w", err)
	}
	secret := WorkloadEnrollmentTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return IssuedWorkloadEnrollmentToken{Secret: secret, Hash: hashWorkloadSecret(secret)}, nil
}

// HashWorkloadEnrollmentToken returns the SHA-256 hash stored for a token.
func HashWorkloadEnrollmentToken(secret string) []byte { return hashWorkloadSecret(secret) }

// IsWorkloadEnrollmentToken reports whether a token looks like an enrolment token.
func IsWorkloadEnrollmentToken(token string) bool {
	return strings.HasPrefix(token, WorkloadEnrollmentTokenPrefix)
}

func hashWorkloadSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}
