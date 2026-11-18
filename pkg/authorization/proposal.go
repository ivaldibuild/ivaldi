package authorization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ProposalDigest returns the SHA-256 digest, hex-encoded, of a Request's
// Proposal as JSON. An approval records it, so it covers only the proposal
// it was asked for. encoding/json writes struct fields in declaration order
// and map keys sorted, so the same proposal always has the same digest.
func ProposalDigest(proposal any) (string, error) {
	b, err := json.Marshal(proposal)
	if err != nil {
		return "", fmt.Errorf("encode proposal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
