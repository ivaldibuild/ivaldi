package iam

// Events a SigningKey records. Each carries the SigningKey as it is after
// the change: its public key and a reference to its private half, never the
// private key.

// SigningKeyCreated is recorded when an Organization gets a signing key.
type SigningKeyCreated struct {
	SigningKey SigningKeySnapshot `json:"signing_key"`
}

// SigningKeyStatusChanged is recorded when a key starts retiring or is
// retired.
type SigningKeyStatusChanged struct {
	SigningKey     SigningKeySnapshot `json:"signing_key"`
	PreviousStatus SigningKeyStatus   `json:"previous_status"`
}

func (SigningKeyCreated) EventName() string       { return "iam.v1.signing_key.created" }
func (SigningKeyStatusChanged) EventName() string { return "iam.v1.signing_key.status_changed" }
