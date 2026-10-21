package iam

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// SigningAlgorithm is the JWS algorithm a SigningKey signs with.
type SigningAlgorithm string

const (
	AlgorithmES256 SigningAlgorithm = "ES256"
	AlgorithmEdDSA SigningAlgorithm = "EdDSA"
	AlgorithmRS256 SigningAlgorithm = "RS256"
)

// SigningKeyStatus is where a key is in its rotation. An active key signs
// and verifies, a retiring key only verifies, so tokens it signed stay
// valid until they expire, and a retired key does neither.
type SigningKeyStatus string

const (
	SigningKeyActive   SigningKeyStatus = "active"
	SigningKeyRetiring SigningKeyStatus = "retiring"
	SigningKeyRetired  SigningKeyStatus = "retired"
)

// privateJWKMembers are the JWK members that carry private or symmetric key
// material (RFC 7518), which a published key must not have.
var privateJWKMembers = []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"}

// SigningKey is an Organization's key for signing access tokens. Its public
// half is published through JWKS so gateways and runners verify tokens
// offline; its private half is held by the credentials capability or a KMS
// and referred to by PrivateKeyRef. It is identified by its JWKS key ID.
type SigningKey struct {
	kid            string
	organizationID string
	algorithm      SigningAlgorithm
	publicJWK      json.RawMessage
	privateKeyRef  string
	status         SigningKeyStatus
	createdAt      time.Time
	notAfter       *time.Time
}

// SigningKeySnapshot is a SigningKey's state, for repositories and audit records.
type SigningKeySnapshot struct {
	KID            string
	OrganizationID string
	Algorithm      SigningAlgorithm
	PublicJWK      json.RawMessage
	PrivateKeyRef  string
	Status         SigningKeyStatus
	CreatedAt      time.Time
	NotAfter       *time.Time
}

// NewSigningKeyProps describes a SigningKey. A new one leaves Status and
// CreatedAt empty; a repository loading a stored one sets them.
type NewSigningKeyProps struct {
	// KID is the opaque key ID published in JWKS.
	KID            string
	OrganizationID string
	Algorithm      SigningAlgorithm
	// PublicJWK is the public key as a JSON Web Key object.
	PublicJWK     json.RawMessage
	PrivateKeyRef string
	// Status defaults to SigningKeyActive.
	Status    SigningKeyStatus
	CreatedAt *time.Time
	// NotAfter is when verifiers stop accepting the key; nil means not until
	// it is retired.
	NotAfter *time.Time
}

// NewSigningKey returns a SigningKey.
func NewSigningKey(props NewSigningKeyProps) (*SigningKey, error) {
	if props.KID == "" || len(props.KID) > maxKeyIDLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("key ID must be 1-%d bytes", maxKeyIDLength))
	}
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	switch props.Algorithm {
	case AlgorithmES256, AlgorithmEdDSA, AlgorithmRS256:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("signing algorithm %q must be ES256, EdDSA, or RS256", props.Algorithm))
	}
	if err := validatePublicJWK(props.PublicJWK); err != nil {
		return nil, err
	}
	if err := validateRequired("private key reference", props.PrivateKeyRef); err != nil {
		return nil, err
	}
	if props.Status == "" {
		props.Status = SigningKeyActive
	}
	switch props.Status {
	case SigningKeyActive, SigningKeyRetiring, SigningKeyRetired:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("signing key status %q must be active, retiring, or retired", props.Status))
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateOptionalExpiry(props.NotAfter, createdAt); err != nil {
		return nil, err
	}

	return &SigningKey{
		kid:            props.KID,
		organizationID: props.OrganizationID,
		algorithm:      props.Algorithm,
		publicJWK:      copyBytes(props.PublicJWK),
		privateKeyRef:  props.PrivateKeyRef,
		status:         props.Status,
		createdAt:      createdAt,
		notAfter:       clock.CopyTime(props.NotAfter),
	}, nil
}

// validatePublicJWK accepts a JSON object with a key type and no private
// key material.
func validatePublicJWK(raw json.RawMessage) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return NewErrInvalidInput("the public key must be a JSON Web Key object")
	}
	if _, ok := members["kty"]; !ok {
		return NewErrInvalidInput("the public key has no key type (kty)")
	}
	for _, m := range privateJWKMembers {
		if _, ok := members[m]; ok {
			return NewErrInvalidInput(fmt.Sprintf("the public key must not contain private key material (%q)", m))
		}
	}
	return nil
}

func (k *SigningKey) KID() string                 { return k.kid }
func (k *SigningKey) OrganizationID() string      { return k.organizationID }
func (k *SigningKey) Algorithm() SigningAlgorithm { return k.algorithm }
func (k *SigningKey) PublicJWK() json.RawMessage  { return copyBytes(k.publicJWK) }
func (k *SigningKey) PrivateKeyRef() string       { return k.privateKeyRef }
func (k *SigningKey) Status() SigningKeyStatus    { return k.status }
func (k *SigningKey) CreatedAt() time.Time        { return k.createdAt }
func (k *SigningKey) NotAfter() *time.Time        { return clock.CopyTime(k.notAfter) }

// Signs reports whether the key signs new tokens.
func (k *SigningKey) Signs() bool { return k.status == SigningKeyActive && k.Verifies() }

// Verifies reports whether verifiers accept tokens signed with the key now.
func (k *SigningKey) Verifies() bool {
	if k.status == SigningKeyRetired {
		return false
	}
	return k.notAfter == nil || clock.Now().Before(*k.notAfter)
}

func (k *SigningKey) Snapshot() SigningKeySnapshot {
	return SigningKeySnapshot{
		KID:            k.kid,
		OrganizationID: k.organizationID,
		Algorithm:      k.algorithm,
		PublicJWK:      copyBytes(k.publicJWK),
		PrivateKeyRef:  k.privateKeyRef,
		Status:         k.status,
		CreatedAt:      k.createdAt,
		NotAfter:       clock.CopyTime(k.notAfter),
	}
}

// StartRetiring stops an active key from signing, while verifiers keep
// accepting it until notAfter, when the last token it signed expires.
// Calling it on a retiring key does nothing.
func (k *SigningKey) StartRetiring(notAfter time.Time) error {
	switch k.status {
	case SigningKeyRetiring:
		return nil
	case SigningKeyRetired:
		return fmt.Errorf("%w: signing key %s is retired", ErrStatus, k.kid)
	}
	if !notAfter.After(clock.Now()) {
		return NewErrInvalidInput("a retiring key must stay valid until a time in the future")
	}
	k.status = SigningKeyRetiring
	k.notAfter = &notAfter
	return nil
}

// Retire stops verifiers accepting the key. A compromised active key can be
// retired at once, skipping the retiring stage. Retiring a retired key does
// nothing.
func (k *SigningKey) Retire() {
	if k.status != SigningKeyRetired {
		now := clock.Now()
		k.status = SigningKeyRetired
		if k.notAfter == nil || k.notAfter.After(now) {
			k.notAfter = &now
		}
	}
}
