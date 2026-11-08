package provenance

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
)

func validateRequired(name, value string) error {
	if value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	return nil
}

func validateText(name, value string, max int, required bool) error {
	if required && value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	if utf8.RuneCountInString(value) > max {
		return NewErrInvalidInput(fmt.Sprintf("%s must be at most %d characters", name, max))
	}
	return nil
}

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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
