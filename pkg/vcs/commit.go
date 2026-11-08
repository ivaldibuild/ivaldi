package vcs

import "time"

// Commit is one commit in a repository. The provider is the source of truth;
// Ivaldi keeps only these fields, not the commit's content.
type Commit struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	CommittedAt time.Time `json:"committed_at"`
	URL         *string   `json:"url"`
}
