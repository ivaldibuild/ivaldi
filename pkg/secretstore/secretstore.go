// Package secretstore reads secret values from the stores customers keep
// them in: Kubernetes Secrets, AWS Secrets Manager, Vault, and Doppler. It
// only fetches. The runtime agent uses it in the customer's cluster; the
// control plane uses only the kinds, to validate store settings, and never
// fetches (ADR 0004).
package secretstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Kind is a kind of secret store.
type Kind string

// The supported kinds.
const (
	KindKubernetes        Kind = "kubernetes"
	KindAWSSecretsManager Kind = "aws_secrets_manager"
	KindVault             Kind = "vault"
	KindDoppler           Kind = "doppler"
)

// Kinds lists every supported kind.
var Kinds = []Kind{KindKubernetes, KindAWSSecretsManager, KindVault, KindDoppler}

// Ref addresses one secret value.
type Ref struct {
	// Path is the Kubernetes Secret's name, the AWS secret's name or ARN,
	// the path within the Vault mount, or the Doppler secret's name.
	Path string
	// Field is the key inside a map or JSON secret; empty for the whole
	// value.
	Field string
	// Version pins a provider version; empty means the latest.
	Version string
}

func (r Ref) String() string {
	out := r.Path
	if r.Field != "" {
		out += "#" + r.Field
	}
	if r.Version != "" {
		out += "@" + r.Version
	}
	return out
}

// Value is a resolved secret value with the version it came from.
type Value struct {
	Value string
	// Version is the provider's version of the value, empty for a store
	// without versions.
	Version string
	Source  Ref
}

// Store reads secret values.
type Store interface {
	Kind() Kind
	// Versions reports whether the store can pin a version.
	Versions() bool
	// Get reads one value. Errors match ErrNotFound, ErrFieldMissing,
	// ErrVersionGone, ErrDenied, or ErrUnavailable, and never contain the
	// value.
	Get(ctx context.Context, ref Ref) (Value, error)
}

// BatchStore reads several values in fewer calls than one per value.
type BatchStore interface {
	Store
	// GetMany reads refs in order, failing on the first that fails.
	GetMany(ctx context.Context, refs []Ref) ([]Value, error)
}

// The errors a Store returns.
var (
	ErrNotFound     = errors.New("the secret does not exist")
	ErrFieldMissing = errors.New("the secret has no such field")
	ErrVersionGone  = errors.New("the secret version does not exist")
	ErrDenied       = errors.New("the store refused access")
	// ErrUnavailable is a failure worth retrying: the store is
	// unreachable, throttling, or failing.
	ErrUnavailable = errors.New("the store is unavailable")
)

// Error is a Store failure naming the store and reference, never the value.
type Error struct {
	Kind Kind
	Ref  Ref
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s secret %s: %v", e.Kind, e.Ref, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// Transient reports whether err is worth retrying.
func Transient(err error) bool { return errors.Is(err, ErrUnavailable) }

func fail(kind Kind, ref Ref, err error) error { return &Error{Kind: kind, Ref: ref, Err: err} }

// field picks a field out of a secret stored as JSON. With no field it
// returns the raw value.
func field(raw string, name string) (string, error) {
	if name == "" {
		return raw, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return "", fmt.Errorf("%w: the value is not a JSON object", ErrFieldMissing)
	}
	v, ok := object[name]
	if !ok {
		return "", ErrFieldMissing
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%w: field %s cannot be encoded", ErrFieldMissing, name)
	}
	return string(encoded), nil
}

// GetMany reads refs from store, in one batch when it supports it.
func GetMany(ctx context.Context, store Store, refs []Ref) ([]Value, error) {
	if batch, ok := store.(BatchStore); ok {
		return batch.GetMany(ctx, refs)
	}
	out := make([]Value, 0, len(refs))
	for _, ref := range refs {
		v, err := store.Get(ctx, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
