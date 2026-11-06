package team

import (
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// slugOrDerived returns s, or a slug made from displayName when s is empty,
// after checking it.
func slugOrDerived(s, displayName string) (string, error) {
	if s == "" {
		s = slug.Create(displayName)
	}
	if err := validateSlug(s); err != nil {
		return "", err
	}
	return s, nil
}

func validateSlug(s string) error {
	if err := slug.Validate(s); err != nil {
		return NewErrInvalidInput(err.Error())
	}
	return nil
}

func validateDisplayName(s string) error {
	if s == "" || utf8.RuneCountInString(s) > maxDisplayNameLength {
		return NewErrInvalidInput(fmt.Sprintf("display name must be 1-%d characters", maxDisplayNameLength))
	}
	return nil
}

func validateRequired(name, value string) error {
	if value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	return nil
}

// newRoot returns an aggregate's identity: storedID, a stored row's ID given
// to a New… constructor, which must have the type's prefix, or a new ID.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, NewErrInvalidInput(err.Error())
	}
	return r, nil
}

// optionalString returns a copy of s, with "" meaning none.
func optionalString(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	c := *s
	return &c
}

func copyString(s *string) *string {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// set assigns *v to *dst when v is given and differs, and reports whether it
// changed anything, so a method records an event only for a real change.
func set[T comparable](dst *T, v *T) bool {
	if v == nil || *dst == *v {
		return false
	}
	*dst = *v
	return true
}

// setOptional is set for an optional string, where an empty v clears it.
func setOptional(dst **string, v *string) bool {
	if v == nil {
		return false
	}
	next := optionalString(v)
	if equalPtr(*dst, next) {
		return false
	}
	*dst = next
	return true
}

// equalPtr reports whether a and b are both nil or point to equal values.
func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// anyTrue reports whether any of changes is true. Every argument is
// evaluated, so it combines several set calls without short-circuiting.
func anyTrue(changes ...bool) bool {
	return slices.Contains(changes, true)
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
