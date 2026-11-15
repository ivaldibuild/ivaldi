// Package credentials is the control plane's credentials capability (ADR
// 0004): where the secrets an authorized actor may use are kept. It stores
// references, never secrets.
//
// So far it holds Secret Stores: the secret managers a runtime agent reads
// an Application's secret variables from (docs/design/k8s-agent.md).
package credentials

import "github.com/ivaldibuild/ivaldi/pkg/id"

func init() {
	id.MustRegister(SecretStore{}.Prefix(), "credentials.SecretStore")
}
