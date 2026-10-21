package iam

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// domainLabel is one DNS label: letters, digits, and inner hyphens.
var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// OrganizationDomain is an email domain an Organization claims. Once
// verified, it routes sign-ins to the Organization's identity provider, and
// lets the Organization link new members to existing accounts with an email
// in it. Several Organizations may claim a domain, but only one can hold it
// verified. It is identified by its Organization and domain.
type OrganizationDomain struct {
	organizationID string
	domain         string
	// verificationTokenHash is the SHA-256 hash of the DNS TXT token that
	// proves ownership.
	verificationTokenHash []byte
	verifiedAt            *time.Time
	createdAt             time.Time
}

// OrganizationDomainSnapshot is an OrganizationDomain's state, for
// repositories and audit records.
type OrganizationDomainSnapshot struct {
	OrganizationID        string
	Domain                string
	VerificationTokenHash []byte
	VerifiedAt            *time.Time
	CreatedAt             time.Time
}

// NewOrganizationDomainProps describes an OrganizationDomain. A new one
// leaves VerifiedAt and CreatedAt nil; a repository loading a stored one
// sets them.
type NewOrganizationDomainProps struct {
	OrganizationID string
	// Domain is stored in lowercase.
	Domain                string
	VerificationTokenHash []byte
	VerifiedAt            *time.Time
	CreatedAt             *time.Time
}

// NewOrganizationDomain returns an OrganizationDomain.
func NewOrganizationDomain(props NewOrganizationDomainProps) (*OrganizationDomain, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	domain := strings.ToLower(props.Domain)
	if err := validateDomain(domain); err != nil {
		return nil, err
	}
	if props.VerificationTokenHash != nil {
		if err := validateTokenHash("verification token hash", props.VerificationTokenHash); err != nil {
			return nil, err
		}
	}
	if props.VerifiedAt == nil && props.VerificationTokenHash == nil {
		return nil, NewErrInvalidInput("an unverified domain needs a verification token hash")
	}

	return &OrganizationDomain{
		organizationID:        props.OrganizationID,
		domain:                domain,
		verificationTokenHash: copyBytes(props.VerificationTokenHash),
		verifiedAt:            clock.CopyTime(props.VerifiedAt),
		createdAt:             timeOr(props.CreatedAt, clock.Now()),
	}, nil
}

// validateDomain accepts a lowercase DNS name with at least two labels.
func validateDomain(domain string) error {
	labels := strings.Split(domain, ".")
	if len(domain) > maxDomainLength || len(labels) < 2 {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a domain name", domain))
	}
	for _, l := range labels {
		if !domainLabel.MatchString(l) {
			return NewErrInvalidInput(fmt.Sprintf("%q is not a domain name", domain))
		}
	}
	return nil
}

func (d *OrganizationDomain) OrganizationID() string { return d.organizationID }
func (d *OrganizationDomain) Domain() string         { return d.domain }
func (d *OrganizationDomain) VerificationTokenHash() []byte {
	return copyBytes(d.verificationTokenHash)
}
func (d *OrganizationDomain) VerifiedAt() *time.Time { return clock.CopyTime(d.verifiedAt) }
func (d *OrganizationDomain) CreatedAt() time.Time   { return d.createdAt }

func (d *OrganizationDomain) Verified() bool { return d.verifiedAt != nil }

// Matches reports whether email's domain is this one, ignoring case.
func (d *OrganizationDomain) Matches(email string) bool {
	at := strings.LastIndexByte(email, '@')
	return at >= 0 && strings.EqualFold(email[at+1:], d.domain)
}

func (d *OrganizationDomain) Snapshot() OrganizationDomainSnapshot {
	return OrganizationDomainSnapshot{
		OrganizationID:        d.organizationID,
		Domain:                d.domain,
		VerificationTokenHash: copyBytes(d.verificationTokenHash),
		VerifiedAt:            clock.CopyTime(d.verifiedAt),
		CreatedAt:             d.createdAt,
	}
}

// Rechallenge replaces the verification token and marks the domain
// unverified until the new token is found.
func (d *OrganizationDomain) Rechallenge(tokenHash []byte) error {
	if err := validateTokenHash("verification token hash", tokenHash); err != nil {
		return err
	}
	d.verificationTokenHash = copyBytes(tokenHash)
	d.verifiedAt = nil
	return nil
}

// Verify records that the verification token was found in DNS. Verifying a
// verified domain does nothing.
func (d *OrganizationDomain) Verify() {
	if !d.Verified() {
		now := clock.Now()
		d.verifiedAt = &now
	}
}
