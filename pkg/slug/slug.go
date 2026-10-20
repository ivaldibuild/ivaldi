// Package slug creates and validates the slugs Ivaldi uses in URLs: 1 to 63
// lowercase ASCII letters and digits, with single hyphens between them.
package slug

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxLength is the longest slug allowed, the length of a DNS label.
const MaxLength = 63

var format = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Create derives a slug from input, such as a display name: it lowercases
// it, keeps ASCII letters and digits, turns each run of anything else into
// one hyphen, and cuts the result to MaxLength. It returns "" when input has
// no ASCII letters or digits, which Validate rejects.
func Create(input string) string {
	var b strings.Builder
	b.Grow(len(input))

	lastWasHyphen := true // trim leading hyphens
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasHyphen = false
		case !lastWasHyphen:
			b.WriteByte('-')
			lastWasHyphen = true
		}
	}

	s := b.String()
	if len(s) > MaxLength {
		s = s[:MaxLength]
	}
	return strings.TrimSuffix(s, "-")
}

// Validate reports whether s is a valid slug.
func Validate(s string) error {
	if len(s) == 0 || len(s) > MaxLength || !format.MatchString(s) {
		return fmt.Errorf("slug %q must be 1-%d lowercase letters, digits, and single hyphens", s, MaxLength)
	}
	return nil
}
