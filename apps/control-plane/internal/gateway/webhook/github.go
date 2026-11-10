package webhook

import (
	"strconv"
	"time"
)

// githubWebhook is the part of a GitHub webhook payload the gateway reads.
type githubWebhook struct {
	Action       string              `json:"action"`
	Installation *githubInstallation `json:"installation"`
	Repository   *githubRepository   `json:"repository"`
}

type githubInstallation struct {
	ID        int64     `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type githubRepository struct {
	ID            int64     `json:"id"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	Visibility    string    `json:"visibility"`
	Private       bool      `json:"private"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	Archived      bool      `json:"archived"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// installationID returns the installation the payload belongs to, as a
// string, or "" when it names none.
func (p githubWebhook) installationID() string {
	if p.Installation == nil || p.Installation.ID == 0 {
		return ""
	}
	return strconv.FormatInt(p.Installation.ID, 10)
}
