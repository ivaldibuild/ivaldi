package secrets

import (
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// Resolved is one secret resolved for a Deployment. Value never leaves the
// agent; the rest is reported as the Deployment's pin.
type Resolved struct {
	Name string
	// Path is the secret's path with its bindings filled in, as it was read.
	Path        string
	Value       string
	Version     string
	Fingerprint string
	ValuesPath  string
	AsValue     bool
}

// Pin is what the Deployment records about the secret: never the value.
func (r Resolved) Pin() deploy.SecretPin {
	deliveredAs := "secret"
	if r.AsValue {
		deliveredAs = "value"
	}
	return deploy.SecretPin{
		Name: r.Name, Path: r.Path, Version: r.Version, Fingerprint: r.Fingerprint, DeliveredAs: deliveredAs,
	}
}

// Credential is where a named credential is kept: a key of a Secret in the
// agent's namespace.
type Credential struct {
	Secret string
	Key    string
}

// opening is what a store is opened for: a Deployment in a namespace, with
// the JWT the control plane granted when the store is federated, or the
// credential it minted when the store is brokered. mask masks a credential
// the store gets in exchange, until the values are read.
type opening struct {
	deploymentID string
	namespace    string
	jwt          string
	credential   *apiclient.BrokeredCredentialView
	mask         func(string)
}
