package versioncontrol

import (
	"fmt"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
)

// parseURL parses an absolute http(s) URL.
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q must be an absolute http or https URL", raw))
	}
	return u, nil
}

// validateRequired reports an empty required value.
func validateRequired(name, value string) error {
	if value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	return nil
}

// validateText checks a value against a column's maximum length.
func validateText(name, value string, max int, required bool) error {
	if required && value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	if utf8.RuneCountInString(value) > max {
		return NewErrInvalidInput(fmt.Sprintf("%s must be at most %d characters", name, max))
	}
	return nil
}

// newRoot returns an aggregate's identity: storedID when a repository
// rebuilds a stored object, or a new ID otherwise.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, NewErrInvalidInput(err.Error())
	}
	return r, nil
}

func copyString(s *string) *string {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
