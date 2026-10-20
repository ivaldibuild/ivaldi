package release

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidVersion means a version or version range could not be parsed.
var ErrInvalidVersion = errors.New("invalid version")

// maxVersionParts is how many dot-separated numbers a version may have:
// three for SemVer, and a fourth for the four-part versions .NET and NuGet use.
const maxVersionParts = 4

// Version is a parsed version: one to four numbers, an optional pre-release
// tag, and optional build metadata, such as 1.4.0-beta.2+abc123. A leading
// "v", as in a Git tag, is accepted and dropped.
type Version struct {
	// Parts holds the numbers. Missing parts compare as 0, so 1.4 equals 1.4.0.
	Parts []uint64
	// PreRelease is the tag after "-", such as "beta.2", or "" for none.
	PreRelease string
	// Build is the metadata after "+". It is ignored when comparing.
	Build string
	raw   string
}

// ParseVersion parses s as a Version.
func ParseVersion(s string) (Version, error) {
	v := Version{raw: s}
	rest := strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		v.Build = rest[i+1:]
		rest = rest[:i]
		if !validIdentifiers(v.Build, false) {
			return Version{}, fmt.Errorf("%w: %q has invalid build metadata", ErrInvalidVersion, s)
		}
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		v.PreRelease = rest[i+1:]
		rest = rest[:i]
		if !validIdentifiers(v.PreRelease, true) {
			return Version{}, fmt.Errorf("%w: %q has an invalid pre-release tag", ErrInvalidVersion, s)
		}
	}
	parts := strings.Split(rest, ".")
	if len(parts) > maxVersionParts {
		return Version{}, fmt.Errorf("%w: %q has more than %d numbers", ErrInvalidVersion, s, maxVersionParts)
	}
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("%w: %q must be numbers separated by dots, without leading zeros", ErrInvalidVersion, s)
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %q must be numbers separated by dots", ErrInvalidVersion, s)
		}
		v.Parts = append(v.Parts, n)
	}
	return v, nil
}

// validIdentifiers reports whether s is dot-separated identifiers of ASCII
// letters, digits, and hyphens. Numeric pre-release identifiers may not have
// leading zeros.
func validIdentifiers(s string, preRelease bool) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for _, r := range id {
			switch {
			case r >= '0' && r <= '9':
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-':
				numeric = false
			default:
				return false
			}
		}
		if preRelease && numeric && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

// String returns the version as it was parsed.
func (v Version) String() string { return v.raw }

// Compare returns -1, 0, or 1 as v is lower than, equal to, or higher than w,
// following SemVer precedence: numbers first, then a version with a
// pre-release tag is lower than the same version without one.
func (v Version) Compare(w Version) int {
	for i := range max(len(v.Parts), len(w.Parts)) {
		a, b := part(v.Parts, i), part(w.Parts, i)
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.PreRelease == w.PreRelease:
		return 0
	case v.PreRelease == "":
		return 1
	case w.PreRelease == "":
		return -1
	}
	return comparePreRelease(v.PreRelease, w.PreRelease)
}

func part(parts []uint64, i int) uint64 {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}

// comparePreRelease compares two pre-release tags identifier by identifier:
// numeric identifiers numerically and below alphanumeric ones, others in
// ASCII order, and a shorter tag below a longer one it is a prefix of.
func comparePreRelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		x, y := as[i], bs[i]
		xn, xerr := strconv.ParseUint(x, 10, 64)
		yn, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// VersionRange is a range of versions in interval notation, as NuGet and
// Octopus write them:
//
//	1.0          1.0 or higher
//	[1.0]        exactly 1.0
//	[1.0,2.0)    1.0 or higher, below 2.0
//	(1.0,)       above 1.0
//	(,2.0]       2.0 or lower
type VersionRange struct {
	// Min and Max are nil when that side is unbounded.
	Min, Max                   *Version
	MinInclusive, MaxInclusive bool
}

// ParseVersionRange parses s as a VersionRange.
func ParseVersionRange(s string) (VersionRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return VersionRange{}, fmt.Errorf("%w: the version range is empty", ErrInvalidVersion)
	}
	if s[0] != '[' && s[0] != '(' {
		v, err := ParseVersion(s)
		if err != nil {
			return VersionRange{}, err
		}
		return VersionRange{Min: &v, MinInclusive: true}, nil
	}
	last := s[len(s)-1]
	if len(s) < 3 || (last != ']' && last != ')') {
		return VersionRange{}, fmt.Errorf("%w: range %q must start with [ or ( and end with ] or )", ErrInvalidVersion, s)
	}
	r := VersionRange{MinInclusive: s[0] == '[', MaxInclusive: last == ']'}
	inner := s[1 : len(s)-1]

	lower, upper, hasComma := strings.Cut(inner, ",")
	if !hasComma {
		if !r.MinInclusive || !r.MaxInclusive {
			return VersionRange{}, fmt.Errorf("%w: range %q names one version, so it must be written [%s]", ErrInvalidVersion, s, inner)
		}
		v, err := ParseVersion(strings.TrimSpace(inner))
		if err != nil {
			return VersionRange{}, err
		}
		r.Min, r.Max = &v, &v
		return r, nil
	}
	if strings.Contains(upper, ",") {
		return VersionRange{}, fmt.Errorf("%w: range %q has more than one comma", ErrInvalidVersion, s)
	}
	lower, upper = strings.TrimSpace(lower), strings.TrimSpace(upper)
	if lower == "" && upper == "" {
		return VersionRange{}, fmt.Errorf("%w: range %q has no bounds", ErrInvalidVersion, s)
	}
	if lower != "" {
		v, err := ParseVersion(lower)
		if err != nil {
			return VersionRange{}, err
		}
		r.Min = &v
	}
	if upper != "" {
		v, err := ParseVersion(upper)
		if err != nil {
			return VersionRange{}, err
		}
		r.Max = &v
	}
	if r.Min != nil && r.Max != nil {
		c := r.Min.Compare(*r.Max)
		if c > 0 || (c == 0 && !(r.MinInclusive && r.MaxInclusive)) {
			return VersionRange{}, fmt.Errorf("%w: range %q contains no versions", ErrInvalidVersion, s)
		}
	}
	return r, nil
}

// Contains reports whether v is in the range.
func (r VersionRange) Contains(v Version) bool {
	if r.Min != nil {
		c := v.Compare(*r.Min)
		if c < 0 || (c == 0 && !r.MinInclusive) {
			return false
		}
	}
	if r.Max != nil {
		c := v.Compare(*r.Max)
		if c > 0 || (c == 0 && !r.MaxInclusive) {
			return false
		}
	}
	return true
}
