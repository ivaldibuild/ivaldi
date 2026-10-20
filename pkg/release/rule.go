package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ErrInvalidRule means a Channel rule's kind or configuration is not valid.
var ErrInvalidRule = errors.New("invalid channel rule")

// RuleKind selects what a Channel rule checks.
type RuleKind string

const (
	// RuleVersion matches the Release's version, or the version of one
	// artifact's Build. Its configuration is a VersionRule.
	RuleVersion RuleKind = "version"
	// RuleGitRef matches the Git refs the Release's Builds were built from.
	// Its configuration is a GitRefRule.
	RuleGitRef RuleKind = "git_ref"
)

// VersionRule configures a version rule. It sets either Range, with an
// optional PreRelease pattern, or Pattern, for versions that are not SemVer.
type VersionRule struct {
	// Range is a VersionRange, such as "[2.0,3.0)".
	Range string `json:"range,omitempty"`
	// PreRelease is a regular expression the pre-release tag must match:
	// "^$" for versions without one, or "^beta" for betas.
	PreRelease *string `json:"pre_release,omitempty"`
	// Pattern is a regular expression the whole version string must match,
	// such as `^build-\d+$`.
	Pattern string `json:"pattern,omitempty"`
}

// GitRefRule configures a git_ref rule.
type GitRefRule struct {
	// Patterns are globs a Build's ref must match one of, such as
	// "refs/heads/main" or "refs/heads/hotfix/**".
	Patterns []string `json:"patterns"`
}

// maxRulePatterns bounds the patterns one git_ref rule may list.
const maxRulePatterns = 50

// NormalizeRuleConfig checks config for a rule of the given kind and
// returns it in canonical JSON, without unknown fields or extra whitespace.
func NormalizeRuleConfig(kind RuleKind, config []byte) ([]byte, error) {
	switch kind {
	case RuleVersion:
		var r VersionRule
		if err := decodeStrict(config, &r); err != nil {
			return nil, err
		}
		if err := r.validate(); err != nil {
			return nil, err
		}
		return json.Marshal(r)
	case RuleGitRef:
		var r GitRefRule
		if err := decodeStrict(config, &r); err != nil {
			return nil, err
		}
		if err := r.validate(); err != nil {
			return nil, err
		}
		return json.Marshal(r)
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidRule, kind)
	}
}

func decodeStrict(config []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(config))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRule, err)
	}
	if d.More() {
		return fmt.Errorf("%w: configuration must be one JSON object", ErrInvalidRule)
	}
	return nil
}

func (r VersionRule) validate() error {
	switch {
	case r.Range == "" && r.Pattern == "":
		return fmt.Errorf("%w: a version rule needs a range or a pattern", ErrInvalidRule)
	case r.Range != "" && r.Pattern != "":
		return fmt.Errorf("%w: a version rule takes a range or a pattern, not both", ErrInvalidRule)
	case r.Pattern != "" && r.PreRelease != nil:
		return fmt.Errorf("%w: a pre-release pattern applies only to a range", ErrInvalidRule)
	}
	if r.Range != "" {
		if _, err := ParseVersionRange(r.Range); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRule, err)
		}
	}
	if r.PreRelease != nil {
		if _, err := regexp.Compile(*r.PreRelease); err != nil {
			return fmt.Errorf("%w: pre-release pattern: %w", ErrInvalidRule, err)
		}
	}
	if r.Pattern != "" {
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("%w: version pattern: %w", ErrInvalidRule, err)
		}
	}
	return nil
}

func (r GitRefRule) validate() error {
	if len(r.Patterns) == 0 || len(r.Patterns) > maxRulePatterns {
		return fmt.Errorf("%w: a git_ref rule needs 1-%d patterns", ErrInvalidRule, maxRulePatterns)
	}
	for _, p := range r.Patterns {
		if !strings.HasPrefix(p, "refs/") || strings.ContainsAny(p, " \t\n") {
			return fmt.Errorf("%w: git ref pattern %q must be a full ref such as refs/heads/main", ErrInvalidRule, p)
		}
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("%w: git ref pattern %q: %w", ErrInvalidRule, p, err)
		}
	}
	return nil
}
