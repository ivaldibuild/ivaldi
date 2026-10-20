package delivery

import (
	"fmt"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/id"
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

// validateOwnID checks an ID given to a New… constructor: a stored row's ID,
// which must have the type's prefix.
func validateOwnID(prefix string, given *string) error {
	if given != nil && !id.Valid(prefix, *given) {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a valid %s_ ID", *given, prefix))
	}
	return nil
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

func copyInt(n *int) *int {
	if n == nil {
		return nil
	}
	c := *n
	return &c
}

func copyLabels(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return maps.Clone(m)
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

func sortedStrings(s []string) []string {
	return slices.Sorted(slices.Values(s))
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
