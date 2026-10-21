package config

import (
	"fmt"
	"os"
	"strings"
)

// Secret is a configuration value that must not be printed. Formatting or
// marshaling it gives "[redacted]", so a Config can be logged whole; Reveal
// returns the value for the one place that uses it.
type Secret string

const redacted = "[redacted]"

// Reveal returns the secret's value.
func (s Secret) Reveal() string { return string(s) }

func (s Secret) String() string   { return mask(s) }
func (s Secret) GoString() string { return mask(s) }

// MarshalText keeps the value out of JSON, YAML, and other encodings.
func (s Secret) MarshalText() ([]byte, error) { return []byte(mask(s)), nil }

func mask(s Secret) string {
	if s == "" {
		return ""
	}
	return redacted
}

// resolveSecret returns value, or the contents of file without a trailing
// newline. Setting both is an error, so it is always clear which one is used.
// Setting neither returns "".
func resolveSecret(name string, value Secret, file string) (Secret, error) {
	switch {
	case value != "" && file != "":
		return "", fmt.Errorf("%s: set the value or the file, not both", name)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return Secret(strings.TrimRight(string(b), "\r\n")), nil
	default:
		return value, nil
	}
}
