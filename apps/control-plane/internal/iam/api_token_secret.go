package iam

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// APITokenPrefix starts every API token, so the API can tell one from an
// access token without parsing it, and secret scanners can find leaked ones.
const APITokenPrefix = "ivt_"

// apiTokenSecretBytes is how much randomness a token carries.
const apiTokenSecretBytes = 32

// apiTokenDisplayChars is how much of a token is kept to tell tokens apart.
const apiTokenDisplayChars = 12

// IssuedAPIToken is a new token's secret, which is shown once and never
// stored, with what is stored instead: a short prefix and the hash.
type IssuedAPIToken struct {
	Secret string
	Prefix string
	Hash   []byte
}

// NewAPITokenSecret generates a token: APITokenPrefix and 32 random bytes,
// base64url-encoded.
func NewAPITokenSecret() (IssuedAPIToken, error) {
	b := make([]byte, apiTokenSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return IssuedAPIToken{}, fmt.Errorf("generate api token: %w", err)
	}
	secret := APITokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return IssuedAPIToken{Secret: secret, Prefix: secret[:apiTokenDisplayChars], Hash: HashAPIToken(secret)}, nil
}

// HashAPIToken returns the SHA-256 hash stored for a token. The token has
// 256 bits of randomness, so a fast hash is enough.
func HashAPIToken(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// IsAPIToken reports whether a bearer token looks like an API token.
func IsAPIToken(token string) bool {
	return strings.HasPrefix(token, APITokenPrefix)
}
