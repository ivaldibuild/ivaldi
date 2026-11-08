package vcs

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
