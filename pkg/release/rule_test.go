package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRuleConfig(t *testing.T) {
	valid := []struct {
		kind RuleKind
		in   string
		want string
	}{
		{RuleVersion, `{ "range": "[2.0,3.0)", "pre_release": "^$" }`, `{"range":"[2.0,3.0)","pre_release":"^$"}`},
		{RuleVersion, `{"pattern": "^build-\\d+$"}`, `{"pattern":"^build-\\d+$"}`},
		{RuleGitRef, `{"patterns": ["refs/heads/main", "refs/heads/hotfix/**"]}`, `{"patterns":["refs/heads/main","refs/heads/hotfix/**"]}`},
	}
	for _, tt := range valid {
		got, err := NormalizeRuleConfig(tt.kind, []byte(tt.in))
		require.NoError(t, err, tt.in)
		assert.JSONEq(t, tt.want, string(got))
		assert.Equal(t, tt.want, string(got), "canonical form")
	}

	invalid := []struct {
		kind RuleKind
		in   string
	}{
		{"branch", `{}`},
		{RuleVersion, `{}`},
		{RuleVersion, `not json`},
		{RuleVersion, `{"range": "1.0"} {}`},
		{RuleVersion, `{"range": "1.0", "extra": true}`},
		{RuleVersion, `{"range": "1.0", "pattern": "x"}`},
		{RuleVersion, `{"pattern": "x", "pre_release": "^$"}`},
		{RuleVersion, `{"range": "[2.0,1.0]"}`},
		{RuleVersion, `{"range": "1.0", "pre_release": "("}`},
		{RuleVersion, `{"pattern": "("}`},
		{RuleGitRef, `{"patterns": []}`},
		{RuleGitRef, `{"patterns": ["main"]}`},
		{RuleGitRef, `{"patterns": ["refs/heads/[main"]}`},
		{RuleGitRef, `{"patterns": ["refs/heads/a b"]}`},
	}
	for _, tt := range invalid {
		_, err := NormalizeRuleConfig(tt.kind, []byte(tt.in))
		assert.ErrorIs(t, err, ErrInvalidRule, "%s %s", tt.kind, tt.in)
	}
}
