// Package vcs is the storage-free part of Ivaldi's version-control
// capability: the types for providers, repositories, and commits, and the
// interfaces the control plane and a provider share.
//
// Ivaldi does not mirror commits. A provider is the source of truth, and a
// capability that needs history, such as delivery comparing two Releases,
// asks through History. The control plane's version-control capability
// implements History over its stored Sources and Connections; a provider
// implements Provider over the provider's API.
package vcs

import "context"

// ProviderKind names a version-control provider.
type ProviderKind string

const (
	ProviderGitHub      ProviderKind = "github"
	ProviderGitLab      ProviderKind = "gitlab"
	ProviderBitbucket   ProviderKind = "bitbucket"
	ProviderGitea       ProviderKind = "gitea"
	ProviderAzureDevOps ProviderKind = "azure_devops"
)

// Valid reports whether k is a provider Ivaldi knows.
func (k ProviderKind) Valid() bool {
	switch k {
	case ProviderGitHub, ProviderGitLab, ProviderBitbucket, ProviderGitea, ProviderAzureDevOps:
		return true
	}
	return false
}

// LookupFrom says where a Connection's provider is reachable. A data-plane
// connection is never contacted from the control plane (ADR 0008).
type LookupFrom string

const (
	LookupFromControlPlane LookupFrom = "control_plane"
	LookupFromDataPlane    LookupFrom = "data_plane"
)

// Endpoint is how to reach one provider account. It carries no secret: a
// provider that needs one resolves CredentialRef, or uses the installation's
// own configured credentials.
type Endpoint struct {
	Provider ProviderKind
	// HostURL is the provider's address, such as https://github.com or a
	// self-hosted server.
	HostURL string
	// InstallationID identifies the provider account, such as a GitHub App
	// installation ID.
	InstallationID string
	// CredentialRef names a credential the provider may need (ADR 0004).
	CredentialRef *string
}

// Provider reads a version-control provider's repositories and history.
type Provider interface {
	// Repository reads one repository by the provider's stable ID.
	Repository(ctx context.Context, at Endpoint, externalRepositoryID string) (Repository, error)
	// Repositories lists the repositories the account's installation can
	// read. It returns ErrTooManyRepositories rather than a partial list.
	Repositories(ctx context.Context, at Endpoint) ([]Repository, error)
	// CommitsBetween returns the commits reachable from to but not from,
	// oldest first.
	CommitsBetween(ctx context.Context, at Endpoint, repo Repository, from, to string) ([]Commit, error)
}
