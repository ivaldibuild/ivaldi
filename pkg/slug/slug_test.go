package slug

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Production", "production"},
		{"  Staging EU (west) ", "staging-eu-west"},
		{"api--v2", "api-v2"},
		{"Café Ünïcode", "caf-n-code"},
		{"!!!", ""},
		{strings.Repeat("a", 62) + " b", strings.Repeat("a", 62)},
		{strings.Repeat("a", 70), strings.Repeat("a", 63)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Create(tt.input)
			assert.Equal(t, tt.want, got)
			if got != "" {
				assert.NoError(t, Validate(got), "Create returns valid slugs")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	for _, s := range []string{"a", "prod-eu-1", strings.Repeat("a", MaxLength)} {
		assert.NoError(t, Validate(s), s)
	}
	for _, s := range []string{"", "Prod", "-a", "a-", "a--b", "a_b", "café", strings.Repeat("a", MaxLength+1)} {
		assert.Error(t, Validate(s), s)
	}
}
