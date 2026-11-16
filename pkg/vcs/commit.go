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

// Comparison is what moving from one commit to another changes.
type Comparison struct {
	// Added are the commits reachable from the new commit but not the old
	// one, oldest first.
	Added []Commit
	// Removed are the commits reachable from the old commit but not the new
	// one, oldest first. They are what a rollback, or a move to a branch that
	// diverged, takes away.
	Removed []Commit
}
