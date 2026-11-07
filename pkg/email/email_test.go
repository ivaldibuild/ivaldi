package email

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	long := strings.Repeat("a", MaxLength-len("@example.com")) + "@example.com"
	for s, want := range map[string]bool{
		"ada@example.com":             true,
		"ada+ci@users.noreply.github": true,
		long:                          true,
		"a" + long:                    false,
		"":                            false,
		"ada":                         false,
		"Ada <ada@example.com>":       false,
		"<ada@example.com>":           false,
		" ada@example.com":            false,
	} {
		if got := Valid(s); got != want {
			t.Errorf("Valid(%q) = %v, want %v", s, got, want)
		}
	}
}
