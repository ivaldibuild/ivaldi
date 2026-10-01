package env

import (
	"fmt"
	"strings"
)

// ParseError is returned when an environment variable value cannot be
// converted to the target field type.
type ParseError struct {
	FieldName string
	EnvVar    string
	Value     string
	TypeName  string
	Err       error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("env: cannot parse %q (from %s) as %s for field %s: %v",
		e.Value, e.EnvVar, e.TypeName, e.FieldName, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// RequiredError is returned when a field tagged with "required" has no
// corresponding environment variable set and no default value.
type RequiredError struct {
	EnvVar string
}

func (e *RequiredError) Error() string {
	return fmt.Sprintf("env: required environment variable %q is not set", e.EnvVar)
}

// NotEmptyError is returned when a field tagged with "notEmpty" has an
// environment variable that is set but empty.
type NotEmptyError struct {
	EnvVar string
}

func (e *NotEmptyError) Error() string {
	return fmt.Sprintf("env: environment variable %q must not be empty", e.EnvVar)
}

// AggregateError wraps multiple errors from a single Parse call.
type AggregateError struct {
	Errors []error
}

func (e *AggregateError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("env: %d error(s):\n  %s", len(e.Errors), strings.Join(msgs, "\n  "))
}
