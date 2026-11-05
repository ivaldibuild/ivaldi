package iam

import (
	"fmt"
	"slices"
	"strings"
)

// ExternalProfile is who an identity provider says signed in, as its
// connector read it from the provider's response. It is a value object.
type ExternalProfile struct {
	// Subject is the person's stable ID at the provider: an OIDC sub, or a
	// GitHub numeric user ID, never a login name that can change.
	Subject string
	Email   string
	// EmailVerified is the provider's word that the person controls Email.
	// Emails are only trusted when it is set.
	EmailVerified bool
	DisplayName   string
	// HostedDomain is a Google Workspace account's domain, from the ID
	// token's hd claim; consumer accounts have none.
	HostedDomain string
	// Organizations are the GitHub organizations the person belongs to, by
	// login, when the provider was asked for them.
	Organizations []string
}

// Admits reports whether the provider lets the person in, by its allow-lists.
// A google or github provider always has one, because anyone in the world
// can sign in there; an oidc provider without any admits everyone. A
// provider that does not sign people in with external profiles, such as the
// builtin one, admits no one. The error matches ErrNotAdmitted.
func (p *IdentityProvider) Admits(profile ExternalProfile) error {
	if profile.Subject == "" {
		return fmt.Errorf("%w: the provider gave no subject", ErrNotAdmitted)
	}
	c := p.config
	verifiedEmail := ""
	if profile.EmailVerified {
		verifiedEmail = strings.ToLower(profile.Email)
	}
	emailAllowed := verifiedEmail != "" && containsFold(c.AllowedEmails, verifiedEmail)

	switch p.protocol {
	case ProtocolGoogle:
		if (profile.HostedDomain != "" && containsFold(c.AllowedDomains, profile.HostedDomain)) || emailAllowed {
			return nil
		}
	case ProtocolGitHub:
		if slices.ContainsFunc(profile.Organizations, func(o string) bool { return containsFold(c.AllowedOrganizations, o) }) || emailAllowed {
			return nil
		}
	case ProtocolOIDC:
		if len(c.AllowedDomains) == 0 && len(c.AllowedEmails) == 0 {
			return nil
		}
		if emailAllowed || (verifiedEmail != "" && containsFold(c.AllowedDomains, emailDomain(verifiedEmail))) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s does not admit this account", ErrNotAdmitted, p.name)
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.EqualFold(v, s) })
}

func emailDomain(email string) string {
	return email[strings.LastIndexByte(email, '@')+1:]
}
