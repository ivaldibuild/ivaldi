package config

import (
	"fmt"
	"os"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Platform configures the components that serve every Organization (ADR
// 0015). It is their only source: bootstrap creates, updates, and suspends
// platform workloads to match it on every start.
type Platform struct {
	Workloads []PlatformWorkload `yaml:"workloads"`
}

// PlatformWorkload is one platform workload, such as the gateway.
type PlatformWorkload struct {
	// Name identifies it among platform workloads; it is kept across starts.
	Name string `yaml:"name"`
	// Component is what it is, such as gateway.
	Component string `yaml:"component"`
	// PublicKeyFiles hold the PEM public keys it signs requests with. Listing
	// a second key and then removing the first rotates it.
	PublicKeyFiles []string `yaml:"public_key_files"`
}

// PublicKeys reads the workload's public keys, DER-encoded (PKIX).
func (w PlatformWorkload) PublicKeys() ([][]byte, error) {
	keys := make([][]byte, 0, len(w.PublicKeyFiles))
	for _, path := range w.PublicKeyFiles {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("platform.workloads %s: %w", w.Name, err)
		}
		public, err := identity.ParseWorkloadPublicKeyPEM(b)
		if err != nil {
			return nil, fmt.Errorf("platform.workloads %s: %s: %w", w.Name, path, err)
		}
		der, err := identity.MarshalWorkloadPublicKeyDER(public)
		if err != nil {
			return nil, fmt.Errorf("platform.workloads %s: %s: %w", w.Name, path, err)
		}
		keys = append(keys, der)
	}
	return keys, nil
}
