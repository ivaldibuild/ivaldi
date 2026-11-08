package delivery

import "time"

// ReleaseChange is one commit between a Target's current Release and the
// Release being compared, with the Session and Work Item it belongs to when
// Ivaldi knows them.
//
// Until the provenance capability records Changes, each commit is reported
// as one ReleaseChange, and SessionID and WorkItemID are nil.
type ReleaseChange struct {
	CommitSHA   string    `json:"commit_sha"`
	SourceID    string    `json:"source_id"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	CommittedAt time.Time `json:"committed_at"`
	URL         *string   `json:"url"`
	SessionID   *string   `json:"session_id"`
	WorkItemID  *string   `json:"work_item_id"`
}
