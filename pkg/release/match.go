package release

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Rule is a Channel rule to evaluate: its kind, the artifact it is scoped
// to (nil for the whole Release), and its configuration as
// NormalizeRuleConfig accepts it.
type Rule struct {
	Kind       RuleKind
	ArtifactID *string
	Config     json.RawMessage
}

// Candidate is a Release as Channel rules see it: its version and, per
// artifact, the Build it carries.
type Candidate struct {
	Version string
	Builds  []CandidateBuild
}

// CandidateBuild is one artifact's Build in a Candidate.
type CandidateBuild struct {
	ArtifactID string
	// ArtifactName names the artifact in refusals, such as "api".
	ArtifactName string
	// Version is the version CI reported for the Build, or "" for none.
	Version string
	// SourceRef is the Git ref the Build was built from, such as
	// refs/heads/main, refs/tags/v1.4.0, or refs/pull/42/head, or "" when
	// it is not known.
	SourceRef string
}

func (c Candidate) build(artifactID string) (CandidateBuild, bool) {
	for _, b := range c.Builds {
		if b.ArtifactID == artifactID {
			return b, true
		}
	}
	return CandidateBuild{}, false
}

// Admits evaluates every rule against the candidate. It returns whether all
// of them match and, for each that does not, why, in words fit to show the
// person who asked. A rule whose configuration is invalid is an error.
func Admits(rules []Rule, c Candidate) (bool, []string, error) {
	var reasons []string
	for _, r := range rules {
		reason, err := evaluate(r, c)
		if err != nil {
			return false, nil, err
		}
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return len(reasons) == 0, reasons, nil
}

// evaluate returns why the rule does not match, or "" when it does.
func evaluate(r Rule, c Candidate) (string, error) {
	switch r.Kind {
	case RuleVersion:
		var cfg VersionRule
		if err := decodeStrict(r.Config, &cfg); err != nil {
			return "", err
		}
		if err := cfg.validate(); err != nil {
			return "", err
		}
		return matchVersion(cfg, r.ArtifactID, c), nil
	case RuleGitRef:
		var cfg GitRefRule
		if err := decodeStrict(r.Config, &cfg); err != nil {
			return "", err
		}
		if err := cfg.validate(); err != nil {
			return "", err
		}
		return matchGitRef(cfg, r.ArtifactID, c), nil
	}
	return "", fmt.Errorf("%w: unknown kind %q", ErrInvalidRule, r.Kind)
}

func matchVersion(cfg VersionRule, artifactID *string, c Candidate) string {
	subject, version := "the release version", c.Version
	if artifactID != nil {
		b, ok := c.build(*artifactID)
		if !ok {
			return "the rule needs a build of an artifact the release does not include"
		}
		subject, version = fmt.Sprintf("the %s build's version", b.ArtifactName), b.Version
		if version == "" {
			return fmt.Sprintf("the %s build has no version", b.ArtifactName)
		}
	}
	if cfg.Pattern != "" {
		if !regexp.MustCompile(cfg.Pattern).MatchString(version) {
			return fmt.Sprintf("%s %q does not match %s", subject, version, cfg.Pattern)
		}
		return ""
	}
	v, err := ParseVersion(version)
	if err != nil {
		return fmt.Sprintf("%s %q is not a version a range can compare", subject, version)
	}
	r, _ := ParseVersionRange(cfg.Range)
	if !r.Contains(v) {
		return fmt.Sprintf("%s %s is not in %s", subject, version, cfg.Range)
	}
	if cfg.PreRelease != nil && !regexp.MustCompile(*cfg.PreRelease).MatchString(v.PreRelease) {
		if v.PreRelease == "" {
			return fmt.Sprintf("%s %s has no pre-release tag matching %s", subject, version, *cfg.PreRelease)
		}
		return fmt.Sprintf("%s %s has pre-release tag %q, which does not match %s", subject, version, v.PreRelease, *cfg.PreRelease)
	}
	return ""
}

func matchGitRef(cfg GitRefRule, artifactID *string, c Candidate) string {
	builds := c.Builds
	if artifactID != nil {
		b, ok := c.build(*artifactID)
		if !ok {
			return "the rule needs a build of an artifact the release does not include"
		}
		builds = []CandidateBuild{b}
	}
	for _, b := range builds {
		if b.SourceRef == "" {
			return fmt.Sprintf("the %s build has no Git ref to match %s", b.ArtifactName, strings.Join(cfg.Patterns, ", "))
		}
		if !matchesAnyRef(cfg.Patterns, b.SourceRef) {
			return fmt.Sprintf("the %s build's ref %s does not match %s", b.ArtifactName, b.SourceRef, strings.Join(cfg.Patterns, ", "))
		}
	}
	return ""
}

func matchesAnyRef(patterns []string, ref string) bool {
	for _, p := range patterns {
		if MatchRef(p, ref) {
			return true
		}
	}
	return false
}

// MatchRef reports whether a Git ref matches a glob: * matches within one
// path segment, ** across segments (refs/heads/hotfix/** matches
// refs/heads/hotfix/a/b), ? one character, and [...] a character class.
func MatchRef(pattern, ref string) bool {
	return globRegexp(pattern).MatchString(ref)
}

func globRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			// A leading ^ negates the class, as it does for path.Match.
			class := pattern[i+1 : i+1+end]
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteByte('$')
	return regexp.MustCompile(b.String())
}
