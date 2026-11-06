package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrSlugTaken means another active Team in the Organization uses the
	// slug, or, for a new Agent, another of its Agents does.
	ErrSlugTaken = errors.New("slug already in use")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the Team is archived.
	ErrArchived = errors.New("archived")
	// ErrOwnsWork means the Team still owns active records, which must be
	// handed to another Team first. OwnsWorkError matches it.
	ErrOwnsWork = errors.New("still owns active work")
	// ErrLastTeam means the change would leave an Agent in no active Team.
	// Every Agent belongs to at least one, which is accountable for it.
	ErrLastTeam = errors.New("is an agent's only active team")
)

// ErrInvalidInput describes why input failed validation. It matches ErrInvalid.
type ErrInvalidInput struct {
	Message string
}

func (e *ErrInvalidInput) Error() string {
	return "invalid input: " + e.Message
}

func (e *ErrInvalidInput) Is(target error) bool { return target == ErrInvalid }

func NewErrInvalidInput(message string) *ErrInvalidInput {
	return &ErrInvalidInput{Message: message}
}

// OwnsWorkError lists what a Team still owns. It matches ErrOwnsWork.
type OwnsWorkError struct {
	Owned []Owned
}

func (e *OwnsWorkError) Error() string {
	parts := make([]string, len(e.Owned))
	for i, o := range e.Owned {
		parts[i] = o.String()
	}
	return "still owns " + strings.Join(parts, ", ") + "; hand them to another team first"
}

func (e *OwnsWorkError) Is(target error) bool { return target == ErrOwnsWork }

// CheckOwned asks every Owner what the Team still owns, and refuses with an
// OwnsWorkError listing it, leaving out kinds with nothing active.
func CheckOwned(ctx context.Context, owners []Owner, organizationID, teamID string) error {
	var owned []Owned
	for _, o := range owners {
		got, err := o.ActiveOwned(ctx, organizationID, teamID)
		if err != nil {
			return fmt.Errorf("check owned work: %w", err)
		}
		for _, w := range got {
			if w.Count > 0 {
				owned = append(owned, w)
			}
		}
	}
	if len(owned) > 0 {
		return &OwnsWorkError{Owned: owned}
	}
	return nil
}
