package iam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func provider(t *testing.T, protocol IdentityProtocol, c IdentityProviderConfig) *IdentityProvider {
	t.Helper()
	props := idpProps()
	props.Protocol = protocol
	props.Config = c
	return mustBuild(NewIdentityProvider(props, nil))
}

func TestIdentityProviderAdmits(t *testing.T) {
	google := IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"acme.com"}, AllowedEmails: []string{"Ada@Gmail.com"}}
	github := IdentityProviderConfig{ClientID: "gh", AllowedOrganizations: []string{"Acme"}, AllowedEmails: []string{"grace@example.com"}}
	oidcOpen := idpOIDCConfig()
	oidcRestricted := idpOIDCConfig()
	oidcRestricted.AllowedDomains = []string{"acme.com"}
	oidcRestricted.AllowedEmails = []string{"contractor@example.com"}

	verified := func(email string) ExternalProfile {
		return ExternalProfile{Subject: "s", Email: email, EmailVerified: true}
	}
	unverified := func(email string) ExternalProfile { return ExternalProfile{Subject: "s", Email: email} }

	tests := []struct {
		name     string
		protocol IdentityProtocol
		config   IdentityProviderConfig
		profile  ExternalProfile
		admitted bool
	}{
		{"google workspace domain", ProtocolGoogle, google, ExternalProfile{Subject: "s", Email: "x@acme.com", HostedDomain: "ACME.com"}, true},
		{"google allowed email, any case", ProtocolGoogle, google, verified("ada@gmail.com"), true},
		{"google allowed email unverified", ProtocolGoogle, google, unverified("ada@gmail.com"), false},
		{"google email domain without hd", ProtocolGoogle, google, verified("x@acme.com"), false},
		{"google other workspace", ProtocolGoogle, google, ExternalProfile{Subject: "s", HostedDomain: "evil.com"}, false},

		{"github member of an allowed org", ProtocolGitHub, github, ExternalProfile{Subject: "1", Organizations: []string{"other", "acme"}}, true},
		{"github allowed verified email", ProtocolGitHub, github, verified("grace@example.com"), true},
		{"github allowed email unverified", ProtocolGitHub, github, unverified("grace@example.com"), false},
		{"github member of no allowed org", ProtocolGitHub, github, ExternalProfile{Subject: "1", Organizations: []string{"other"}}, false},

		{"oidc without allow-lists admits anyone", ProtocolOIDC, oidcOpen, unverified("anyone@anywhere.com"), true},
		{"oidc verified email in allowed domain", ProtocolOIDC, oidcRestricted, verified("ada@ACME.com"), true},
		{"oidc allowed email", ProtocolOIDC, oidcRestricted, verified("contractor@example.com"), true},
		{"oidc unverified email in allowed domain", ProtocolOIDC, oidcRestricted, unverified("ada@acme.com"), false},
		{"oidc other domain", ProtocolOIDC, oidcRestricted, verified("ada@evil.com"), false},

		{"no subject", ProtocolOIDC, oidcOpen, ExternalProfile{Email: "a@b.c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider(t, tt.protocol, tt.config).Admits(tt.profile)
			if tt.admitted {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrNotAdmitted)
			}
		})
	}
}

func TestBuiltinProviderAdmitsNoExternalProfile(t *testing.T) {
	props := idpProps()
	props.Protocol = ProtocolBuiltin
	props.Config = IdentityProviderConfig{}
	props.SecretRef = nil
	p := mustBuild(NewIdentityProvider(props, nil))

	assert.ErrorIs(t, p.Admits(ExternalProfile{Subject: "s", Email: "a@b.c", EmailVerified: true}), ErrNotAdmitted)
}
