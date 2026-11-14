// Package secrets resolves the secrets a Deployment uses, in the cluster:
// it reads them from their stores, fingerprints them with a key that never
// leaves the cluster, refuses a rollback whose pinned secret has changed,
// writes the ones delivered as a Secret, and masks every value in logs and
// reports. No value is ever sent to the control plane.
package secrets

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// fingerprintScheme prefixes every fingerprint.
const fingerprintScheme = "hmac-sha256"

// keySize is the size of a fingerprint key.
const keySize = 32

// Fingerprinter keys fingerprints, so a stored fingerprint cannot be
// brute-forced back into a short secret.
type Fingerprinter interface {
	// Fingerprint returns "hmac-sha256:v<key version>:<hex>" for value,
	// with the newest key.
	Fingerprint(value string) string
	// Matches reports whether value has the pinned fingerprint, using the
	// key version the fingerprint names. It fails, by name, when that key
	// is gone.
	Matches(pinned, value string) (bool, error)
}

// newFingerprinter returns a Fingerprinter over keys by version.
func newFingerprinter(keys map[int][]byte) (Fingerprinter, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no fingerprint key")
	}
	newest := 0
	for version := range keys {
		newest = max(newest, version)
	}
	return &fingerprinter{keys: keys, newest: newest}, nil
}

type fingerprinter struct {
	keys   map[int][]byte
	newest int
}

func (f *fingerprinter) Fingerprint(value string) string {
	return f.sign(f.newest, value)
}

func (f *fingerprinter) sign(version int, value string) string {
	mac := hmac.New(sha256.New, f.keys[version])
	mac.Write([]byte(value))
	return fmt.Sprintf("%s:v%d:%s", fingerprintScheme, version, hex.EncodeToString(mac.Sum(nil)))
}

func (f *fingerprinter) Matches(pinned, value string) (bool, error) {
	scheme, rest, ok := strings.Cut(pinned, ":")
	if !ok || scheme != fingerprintScheme {
		return false, fmt.Errorf("fingerprint %q is not an %s fingerprint", pinned, fingerprintScheme)
	}
	label, _, ok := strings.Cut(rest, ":")
	version, err := strconv.Atoi(strings.TrimPrefix(label, "v"))
	if !ok || !strings.HasPrefix(label, "v") || err != nil {
		return false, fmt.Errorf("fingerprint %q names no key version", pinned)
	}
	if _, ok := f.keys[version]; !ok {
		return false, fmt.Errorf("fingerprint key v%d is gone", version)
	}
	return hmac.Equal([]byte(f.sign(version, value)), []byte(pinned)), nil
}

// LoadFingerprinter reads the fingerprint keys from the Secret name in the
// agent's namespace, one per data key "v<N>". When the Secret has none, it
// creates v1 and saves it: the chart creates the Secret empty.
func LoadFingerprinter(ctx context.Context, kube kubernetes.Interface, namespace, name string) (Fingerprinter, error) {
	secrets := kube.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read fingerprint key secret %s/%s: %w", namespace, name, err)
	}
	keys := map[int][]byte{}
	for k, v := range secret.Data {
		version, err := strconv.Atoi(strings.TrimPrefix(k, "v"))
		if !strings.HasPrefix(k, "v") || err != nil || len(v) < keySize {
			continue
		}
		keys[version] = v
	}
	if len(keys) == 0 {
		key := make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data["v1"] = key
		if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("save the new fingerprint key in %s/%s: %w", namespace, name, err)
		}
		keys[1] = key
	}
	return newFingerprinter(keys)
}
