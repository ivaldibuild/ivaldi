package vcs

import (
	"net/url"
	"strings"
)

// ProviderKey identifies something a provider holds, such as a repository
// or an installation, by the provider, its host, and the provider's stable
// ID, such as github:github.com:123456. The ID survives renames and
// transfers.
func ProviderKey(provider ProviderKind, hostURL, externalID string) string {
	host := hostURL
	if u, err := url.Parse(hostURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return strings.Join([]string{string(provider), strings.TrimSuffix(host, "/"), externalID}, ":")
}

// Repository is one repository a provider serves.
type Repository struct {
	Provider      ProviderKind `json:"provider"`
	HostURL       string       `json:"host_url"`
	ExternalID    string       `json:"external_id"`
	FullName      string       `json:"full_name"`
	DefaultBranch string       `json:"default_branch"`
	Visibility    string       `json:"visibility"`
	CloneURL      string       `json:"clone_url"`
	WebURL        string       `json:"web_url"`
}
