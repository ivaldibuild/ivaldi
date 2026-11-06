package release

import "fmt"

// Status is where a Release is in its life: a draft can still change, a
// published Release can be deployed, and a withdrawn one can no longer be
// deployed again.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusWithdrawn Status = "withdrawn"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPublished, StatusWithdrawn:
		return true
	}
	return false
}

// transitions lists where each status may go: draft → published →
// withdrawn. A Release is never unpublished; it is withdrawn instead.
var transitions = map[Status][]Status{
	StatusDraft:     {StatusPublished},
	StatusPublished: {StatusWithdrawn},
}

// CanMove reports whether a Release may go from one status to another.
func CanMove(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// CheckMove returns an error unless a Release may go from one status to another.
func CheckMove(from, to Status) error {
	if !CanMove(from, to) {
		return fmt.Errorf("a %s release cannot become %s", from, to)
	}
	return nil
}
