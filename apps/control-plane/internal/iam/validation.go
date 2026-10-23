package iam

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

func validateSlug(s string) error {
	if err := slug.Validate(s); err != nil {
		return NewErrInvalidInput(err.Error())
	}
	return nil
}

// validateName checks a required name or display name of up to max characters.
func validateName(field, s string, max int) error {
	if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > max {
		return NewErrInvalidInput(fmt.Sprintf("%s must be 1-%d characters", field, max))
	}
	return nil
}

func validateRequired(name, value string) error {
	if value == "" {
		return NewErrInvalidInput(name + " is required")
	}
	return nil
}

// validateID checks a reference to another IAM record, which must carry
// that record's prefix.
func validateID(field, prefix, value string) error {
	if !id.Valid(prefix, value) {
		return NewErrInvalidInput(fmt.Sprintf("%s %q is not a valid %s_ ID", field, value, prefix))
	}
	return nil
}

// newRoot returns an aggregate's identity: storedID, a stored row's ID given
// to a New… constructor, which must have the type's prefix, or a new ID.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, NewErrInvalidInput(err.Error())
	}
	return r, nil
}

// validateTokenHash checks a SHA-256 hash of a token or code. IAM stores
// only hashes, never the raw values.
func validateTokenHash(field string, hash []byte) error {
	if len(hash) != sha256.Size {
		return NewErrInvalidInput(fmt.Sprintf("%s must be a %d-byte SHA-256 hash", field, sha256.Size))
	}
	return nil
}

// validateExpiry checks that a record expires after it was created.
func validateExpiry(expiresAt, createdAt time.Time) error {
	if expiresAt.IsZero() {
		return NewErrInvalidInput("an expiry time is required")
	}
	if !expiresAt.After(createdAt) {
		return NewErrInvalidInput("the expiry time must be after the creation time")
	}
	return nil
}

// validateOptionalExpiry is validateExpiry for records that may never expire.
func validateOptionalExpiry(expiresAt *time.Time, createdAt time.Time) error {
	if expiresAt == nil {
		return nil
	}
	return validateExpiry(*expiresAt, createdAt)
}

// validateHTTPSURL accepts an absolute https URL with a host and no credentials.
func validateHTTPSURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return NewErrInvalidInput(fmt.Sprintf("%s %q must be an https URL", field, raw))
	}
	if u.User != nil {
		return NewErrInvalidInput(fmt.Sprintf("%s must not contain credentials", field))
	}
	return nil
}

func validateMetadata(m map[string]string) error {
	if len(m) > maxMetadataEntries {
		return NewErrInvalidInput(fmt.Sprintf("metadata can have at most %d entries", maxMetadataEntries))
	}
	for k := range m {
		if k == "" {
			return NewErrInvalidInput("metadata keys must not be empty")
		}
	}
	return nil
}

// optionalString returns a copy of s, with "" meaning none.
func optionalString(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	c := *s
	return &c
}

func copyString(s *string) *string {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

func copyMetadata(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return maps.Clone(m)
}

func sortedStrings(s []string) []string {
	return slices.Sorted(slices.Values(s))
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
