// Package state keeps what the agent learned at enrolment in a Kubernetes
// Secret in its own namespace, so a replaced Pod resumes with the same
// credential. It also reads the one-time enrolment token.
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// stateKey is the state Secret's only data key.
const stateKey = "state.json"

// EnrolmentTokenKey is the enrolment Secret's data key holding the token.
const EnrolmentTokenKey = "token"

// ErrNoEnrolmentToken means the enrolment Secret holds no token.
var ErrNoEnrolmentToken = errors.New("the enrolment secret holds no token")

// State is what the agent keeps between restarts. A State without a
// CredentialID holds no credential: the agent has never enrolled, or its
// credential was lost and it is waiting for a new enrolment token.
type State struct {
	ClusterID           string     `json:"cluster_id,omitempty"`
	OrganizationID      string     `json:"organization_id,omitempty"`
	WorkloadPrincipalID string     `json:"workload_principal_id,omitempty"`
	CredentialID        string     `json:"credential_id,omitempty"`
	CredentialKind      string     `json:"credential_kind,omitempty"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	// IssuedAt is when the agent received the current credential, by its own
	// clock; renewal is scheduled from it.
	IssuedAt time.Time `json:"issued_at,omitzero"`
	// WorkloadToken is the bearer token, for the bearer method only.
	WorkloadToken string `json:"workload_token,omitempty"`
	// EnrolmentTokenSHA256 identifies the last enrolment token the agent
	// tried to redeem, successfully or not, so it never tries one twice.
	EnrolmentTokenSHA256 string `json:"enrolment_token_sha256,omitempty"`
}

// HasCredential reports whether the state holds a credential.
func (s State) HasCredential() bool { return s.CredentialID != "" }

// WithoutCredential returns the state with its credential removed, keeping
// which enrolment token was last tried.
func (s State) WithoutCredential() State {
	return State{EnrolmentTokenSHA256: s.EnrolmentTokenSHA256}
}

// HashEnrolmentToken returns the value kept in EnrolmentTokenSHA256.
func HashEnrolmentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Store loads and saves the agent's State.
type Store interface {
	// Load returns the stored State, empty when nothing is stored yet.
	Load(ctx context.Context) (State, error)
	// Save replaces the stored State.
	Save(ctx context.Context, s State) error
}

// EnrolmentTokens reads the one-time enrolment token.
type EnrolmentTokens interface {
	// Token returns the enrolment token, or ErrNoEnrolmentToken.
	Token(ctx context.Context) (string, error)
}

// NewSecretStore returns a Store over the Secret name in namespace. The chart
// creates the Secret; the agent may only get and update it.
func NewSecretStore(client kubernetes.Interface, namespace, name string) Store {
	return &secretStore{client: client, namespace: namespace, name: name}
}

type secretStore struct {
	client    kubernetes.Interface
	namespace string
	name      string
}

func (s *secretStore) Load(ctx context.Context) (State, error) {
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		return State{}, s.wrap("read", err)
	}
	raw := secret.Data[stateKey]
	if len(raw) == 0 {
		return State{}, nil
	}
	var out State
	if err := json.Unmarshal(raw, &out); err != nil {
		return State{}, fmt.Errorf("state secret %s/%s: %s is not valid: %w", s.namespace, s.name, stateKey, err)
	}
	return out, nil
}

func (s *secretStore) Save(ctx context.Context, st State) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	secrets := s.client.CoreV1().Secrets(s.namespace)
	secret, err := secrets.Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		return s.wrap("read", err)
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[stateKey] = raw
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return s.wrap("update", err)
	}
	return nil
}

func (s *secretStore) wrap(verb string, err error) error {
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("state secret %s/%s does not exist; the chart creates it", s.namespace, s.name)
	}
	return fmt.Errorf("%s state secret %s/%s: %w", verb, s.namespace, s.name, err)
}

// NewEnrolmentSecret returns the EnrolmentTokens held in the Secret name in
// namespace, under the key "token".
func NewEnrolmentSecret(client kubernetes.Interface, namespace, name string) EnrolmentTokens {
	return &enrolmentSecret{client: client, namespace: namespace, name: name}
}

type enrolmentSecret struct {
	client    kubernetes.Interface
	namespace string
	name      string
}

func (e *enrolmentSecret) Token(ctx context.Context) (string, error) {
	secret, err := e.client.CoreV1().Secrets(e.namespace).Get(ctx, e.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", ErrNoEnrolmentToken
	}
	if err != nil {
		return "", fmt.Errorf("read enrolment secret %s/%s: %w", e.namespace, e.name, err)
	}
	token := strings.TrimSpace(string(secret.Data[EnrolmentTokenKey]))
	if token == "" {
		return "", ErrNoEnrolmentToken
	}
	return token, nil
}
