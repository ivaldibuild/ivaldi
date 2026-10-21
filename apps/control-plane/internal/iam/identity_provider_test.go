package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func idpOIDCConfig() IdentityProviderConfig {
	return IdentityProviderConfig{Issuer: "https://login.example.com", ClientID: "ivaldi"}
}

func idpProps() NewIdentityProviderProps {
	return NewIdentityProviderProps{
		OrganizationID: testOrg,
		Protocol:       ProtocolOIDC,
		Name:           "Example SSO",
		Config:         idpOIDCConfig(),
		SecretRef:      new("vault://idp/example"),
	}
}

func idpNew(t *testing.T, props NewIdentityProviderProps) *IdentityProvider {
	t.Helper()
	p, err := NewIdentityProvider(props, nil)
	require.NoError(t, err)
	return p
}

func idpManaged(t *testing.T) *IdentityProvider {
	t.Helper()
	props := idpProps()
	props.ManagedByConfiguration = true
	return idpNew(t, props)
}

func TestNewIdentityProvider(t *testing.T) {
	clock.Fix(t, t0)

	props := idpProps()
	props.JITProvisioning = true
	p, err := NewIdentityProvider(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("idp", p.ID()), "a new identity provider gets an idp_ ID: %q", p.ID())
	assert.Equal(t, testOrg, p.OrganizationID())
	assert.Equal(t, ProtocolOIDC, p.Protocol())
	assert.Equal(t, "Example SSO", p.Name())
	assert.Equal(t, idpOIDCConfig(), p.Config())
	assert.False(t, p.ManagedByConfiguration())
	require.NotNil(t, p.SecretRef())
	assert.Equal(t, "vault://idp/example", *p.SecretRef())
	assert.True(t, p.JITProvisioning())
	assert.Equal(t, IdentityProviderActive, p.Status(), "status defaults to active")
	assert.True(t, p.Active())
	assert.Equal(t, t0, p.CreatedAt())
	assert.Equal(t, t0, p.UpdatedAt())
}

func TestNewIdentityProviderWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("idp")
	props := idpProps()
	props.Status = IdentityProviderDisabled
	props.ManagedByConfiguration = true
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)

	p, err := NewIdentityProvider(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, p.ID(), "a given ID is kept")
	assert.Equal(t, IdentityProviderDisabled, p.Status())
	assert.False(t, p.Active())
	assert.True(t, p.ManagedByConfiguration())
	assert.Equal(t, t0, p.CreatedAt(), "given timestamps are kept")
	assert.Equal(t, t1, p.UpdatedAt())
}

func TestNewIdentityProviderEmptySecretRefIsNone(t *testing.T) {
	props := idpProps()
	props.SecretRef = new("")

	p := idpNew(t, props)

	assert.Nil(t, p.SecretRef())
}

func TestNewIdentityProviderAcceptsEveryProtocol(t *testing.T) {
	tests := []struct {
		protocol IdentityProtocol
		config   IdentityProviderConfig
	}{
		{ProtocolOIDC, idpOIDCConfig()},
		{ProtocolSAML, IdentityProviderConfig{MetadataURL: "https://idp.example.com/metadata"}},
		{ProtocolBuiltin, IdentityProviderConfig{}},
		{ProtocolGoogle, IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"example.com"}}},
		{ProtocolGoogle, IdentityProviderConfig{ClientID: "g", AllowedEmails: []string{"ada@example.com"}}},
		{ProtocolGitHub, IdentityProviderConfig{ClientID: "gh", AllowedOrganizations: []string{"ivaldibuild"}}},
		{ProtocolGitHub, IdentityProviderConfig{ClientID: "gh", BaseURL: "https://github.example.com", AllowedEmails: []string{"ada@example.com"}}},
	}
	for _, tt := range tests {
		t.Run(string(tt.protocol), func(t *testing.T) {
			props := idpProps()
			props.Protocol = tt.protocol
			props.Config = tt.config
			if tt.protocol == ProtocolBuiltin {
				props.SecretRef = nil
			}
			p, err := NewIdentityProvider(props, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.protocol, p.Protocol())
		})
	}
}

func TestNewIdentityProviderIsRefused(t *testing.T) {
	google := IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"example.com"}}
	github := IdentityProviderConfig{ClientID: "gh", AllowedOrganizations: []string{"ivaldibuild"}}
	tests := []struct {
		name     string
		protocol IdentityProtocol
		mutate   func(*NewIdentityProviderProps)
		id       *string
	}{
		{"no organization", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.OrganizationID = "" }, nil},
		{"malformed organization", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.OrganizationID = testWorkspace }, nil},
		{"no name", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Name = "" }, nil},
		{"blank name", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Name = "   " }, nil},
		{"name too long", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Name = strings.Repeat("é", maxNameLength+1) }, nil},
		{"unknown protocol", "ldap", func(*NewIdentityProviderProps) {}, nil},
		{"empty protocol", "", func(*NewIdentityProviderProps) {}, nil},
		{"unknown status", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Status = "paused" }, nil},
		{"ID with wrong prefix", ProtocolOIDC, func(*NewIdentityProviderProps) {}, new(id.New("org"))},
		{"malformed ID", ProtocolOIDC, func(*NewIdentityProviderProps) {}, new("idp_nope")},

		{"oidc without issuer", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.Issuer = "" }, nil},
		{"oidc without client ID", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.ClientID = "" }, nil},
		{"oidc http issuer", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.Issuer = "http://login.example.com" }, nil},
		{"oidc issuer with credentials", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.Issuer = "https://u:p@login.example.com" }, nil},
		{"oidc with metadata URL", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.MetadataURL = "https://x.example.com" }, nil},
		{"oidc with allowed emails", ProtocolOIDC, func(p *NewIdentityProviderProps) { p.Config.AllowedEmails = []string{"ada@example.com"} }, nil},

		{"saml without metadata URL", ProtocolSAML, func(p *NewIdentityProviderProps) { p.Config = IdentityProviderConfig{} }, nil},
		{"saml with client ID", ProtocolSAML, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{MetadataURL: "https://idp.example.com/metadata", ClientID: "x"}
		}, nil},
		{"saml http metadata URL", ProtocolSAML, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{MetadataURL: "http://idp.example.com/metadata"}
		}, nil},

		{"builtin with secret", ProtocolBuiltin, func(p *NewIdentityProviderProps) { p.Config = IdentityProviderConfig{} }, nil},
		{"builtin with client ID", ProtocolBuiltin, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{ClientID: "x"}
			p.SecretRef = nil
		}, nil},

		{"google without restriction", ProtocolGoogle, func(p *NewIdentityProviderProps) { p.Config = IdentityProviderConfig{ClientID: "g"} }, nil},
		{"google without client ID", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{AllowedDomains: []string{"example.com"}}
		}, nil},
		{"google with organizations", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = google
			p.Config.AllowedOrganizations = []string{"ivaldibuild"}
		}, nil},
		{"google with issuer", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = google
			p.Config.Issuer = "https://accounts.google.com"
		}, nil},
		{"google with base URL", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = google
			p.Config.BaseURL = "https://github.example.com"
		}, nil},
		{"google uppercase domain", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"Example.com"}}
		}, nil},
		{"google malformed domain", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{ClientID: "g", AllowedDomains: []string{"localhost"}}
		}, nil},
		{"google malformed email", ProtocolGoogle, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{ClientID: "g", AllowedEmails: []string{"Ada <ada@example.com>"}}
		}, nil},

		{"github without restriction", ProtocolGitHub, func(p *NewIdentityProviderProps) { p.Config = IdentityProviderConfig{ClientID: "gh"} }, nil},
		{"github without client ID", ProtocolGitHub, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{AllowedOrganizations: []string{"ivaldibuild"}}
		}, nil},
		{"github with domains", ProtocolGitHub, func(p *NewIdentityProviderProps) {
			p.Config = github
			p.Config.AllowedDomains = []string{"example.com"}
		}, nil},
		{"github http base URL", ProtocolGitHub, func(p *NewIdentityProviderProps) {
			p.Config = github
			p.Config.BaseURL = "http://github.example.com"
		}, nil},
		{"github blank organization", ProtocolGitHub, func(p *NewIdentityProviderProps) {
			p.Config = IdentityProviderConfig{ClientID: "gh", AllowedOrganizations: []string{" "}}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := idpProps()
			props.Protocol = tt.protocol
			tt.mutate(&props)
			_, err := NewIdentityProvider(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestIdentityProviderGettersReturnCopies(t *testing.T) {
	props := idpProps()
	props.Protocol = ProtocolGoogle
	props.Config = IdentityProviderConfig{
		ClientID:       "g",
		AllowedDomains: []string{"example.com"},
		AllowedEmails:  []string{"ada@example.com"},
	}
	p := idpNew(t, props)

	props.Config.AllowedDomains[0] = "given.example.com"
	*props.SecretRef = "vault://changed"
	c := p.Config()
	c.AllowedDomains[0] = "getter.example.com"
	c.AllowedEmails[0] = "eve@example.com"
	*p.SecretRef() = "vault://getter"
	snap := p.Snapshot()
	snap.Config.AllowedDomains[0] = "snapshot.example.com"
	*snap.SecretRef = "vault://snapshot"

	assert.Equal(t, []string{"example.com"}, p.Config().AllowedDomains)
	assert.Equal(t, []string{"ada@example.com"}, p.Config().AllowedEmails)
	assert.Equal(t, "vault://idp/example", *p.SecretRef())
}

func TestIdentityProviderSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	p := idpNew(t, idpProps())

	assert.Equal(t, IdentityProviderSnapshot{
		ID:             p.ID(),
		OrganizationID: testOrg,
		Protocol:       ProtocolOIDC,
		Name:           "Example SSO",
		Config:         idpOIDCConfig(),
		SecretRef:      new("vault://idp/example"),
		Status:         IdentityProviderActive,
		CreatedAt:      t0,
		UpdatedAt:      t0,
	}, p.Snapshot())
}

func TestIdentityProviderUpdate(t *testing.T) {
	clock.Fix(t, t0)
	p := idpNew(t, idpProps())
	clock.Fix(t, t1)

	config := IdentityProviderConfig{Issuer: "https://sso.example.com", ClientID: "ivaldi-2"}
	err := p.Update(UpdateIdentityProviderProps{
		Name:            new("New SSO"),
		Config:          &config,
		SecretRef:       new("vault://idp/new"),
		JITProvisioning: new(true),
	})

	require.NoError(t, err)
	assert.Equal(t, "New SSO", p.Name())
	assert.Equal(t, config, p.Config())
	assert.Equal(t, "vault://idp/new", *p.SecretRef())
	assert.True(t, p.JITProvisioning())
	assert.Equal(t, t0, p.CreatedAt())
	assert.Equal(t, t1, p.UpdatedAt())

	config.ClientID = "changed"
	assert.Equal(t, "ivaldi-2", p.Config().ClientID, "the given config is copied")
}

func TestIdentityProviderUpdateLeavesUnsetFields(t *testing.T) {
	p := idpNew(t, idpProps())

	require.NoError(t, p.Update(UpdateIdentityProviderProps{}))

	assert.Equal(t, "Example SSO", p.Name())
	assert.Equal(t, idpOIDCConfig(), p.Config())
	assert.Equal(t, "vault://idp/example", *p.SecretRef())
}

func TestIdentityProviderUpdateRemovesSecret(t *testing.T) {
	p := idpNew(t, idpProps())

	require.NoError(t, p.Update(UpdateIdentityProviderProps{SecretRef: new("")}))

	assert.Nil(t, p.SecretRef())
}

func TestIdentityProviderUpdateIsRefused(t *testing.T) {
	badConfig := IdentityProviderConfig{ClientID: "x"}
	tests := []struct {
		name     string
		protocol IdentityProtocol
		props    UpdateIdentityProviderProps
	}{
		{"empty name", ProtocolOIDC, UpdateIdentityProviderProps{Name: new("")}},
		{"config the protocol rejects", ProtocolOIDC, UpdateIdentityProviderProps{Config: &badConfig}},
		{"secret on builtin", ProtocolBuiltin, UpdateIdentityProviderProps{SecretRef: new("vault://x")}},
		{"valid name with bad config", ProtocolOIDC, UpdateIdentityProviderProps{Name: new("Renamed"), Config: &badConfig}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := idpProps()
			props.Protocol = tt.protocol
			if tt.protocol == ProtocolBuiltin {
				props.Config = IdentityProviderConfig{}
				props.SecretRef = nil
			}
			p := idpNew(t, props)
			before := p.Snapshot()
			clock.Fix(t, t1)

			assert.ErrorIs(t, p.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, p.Snapshot(), "nothing changes")
		})
	}
}

func TestIdentityProviderManagedRefusesAPIChanges(t *testing.T) {
	p := idpManaged(t)
	before := p.Snapshot()

	assert.ErrorIs(t, p.Update(UpdateIdentityProviderProps{Name: new("Renamed")}), ErrManagedByConfiguration)
	assert.ErrorIs(t, p.Enable(), ErrManagedByConfiguration)
	assert.ErrorIs(t, p.Disable(), ErrManagedByConfiguration)
	assert.Equal(t, before, p.Snapshot())
}

func TestIdentityProviderDisableAndEnable(t *testing.T) {
	clock.Fix(t, t0)
	p := idpNew(t, idpProps())

	clock.Fix(t, t1)
	require.NoError(t, p.Disable())
	assert.Equal(t, IdentityProviderDisabled, p.Status())
	assert.False(t, p.Active())
	assert.Equal(t, t1, p.UpdatedAt())

	clock.Fix(t, t2)
	require.NoError(t, p.Disable(), "disabling a disabled provider does nothing")
	assert.Equal(t, t1, p.UpdatedAt())

	require.NoError(t, p.Enable())
	assert.Equal(t, IdentityProviderActive, p.Status())
	assert.Equal(t, t2, p.UpdatedAt())
}

func TestIdentityProviderEnableActiveDoesNothing(t *testing.T) {
	clock.Fix(t, t0)
	p := idpNew(t, idpProps())
	clock.Fix(t, t1)

	require.NoError(t, p.Enable())

	assert.Equal(t, t0, p.UpdatedAt())
}

func TestIdentityProviderConfigure(t *testing.T) {
	clock.Fix(t, t0)
	props := idpProps()
	props.Status = IdentityProviderDisabled
	p := idpNew(t, props)
	clock.Fix(t, t1)

	config := IdentityProviderConfig{Issuer: "https://sso.example.com", ClientID: "configured"}
	err := p.Configure(UpdateIdentityProviderProps{
		Name:            new("Configured SSO"),
		Config:          &config,
		JITProvisioning: new(true),
	})

	require.NoError(t, err)
	assert.True(t, p.ManagedByConfiguration())
	assert.Equal(t, IdentityProviderActive, p.Status())
	assert.Equal(t, "Configured SSO", p.Name())
	assert.Equal(t, config, p.Config())
	assert.True(t, p.JITProvisioning())
	assert.Nil(t, p.SecretRef(), "a secret not in the configuration is removed")
	assert.Equal(t, t1, p.UpdatedAt())
}

func TestIdentityProviderConfigureKeepsGivenSecretAndReapplies(t *testing.T) {
	p := idpManaged(t)
	config := idpOIDCConfig()

	err := p.Configure(UpdateIdentityProviderProps{
		Name:            new("Example SSO"),
		Config:          &config,
		SecretRef:       new("vault://idp/configured"),
		JITProvisioning: new(false),
	})

	require.NoError(t, err, "a managed provider can be configured again")
	assert.Equal(t, "vault://idp/configured", *p.SecretRef())
}

func TestIdentityProviderConfigureIsRefused(t *testing.T) {
	config := idpOIDCConfig()
	badConfig := IdentityProviderConfig{ClientID: "x"}
	tests := []struct {
		name  string
		props UpdateIdentityProviderProps
	}{
		{"no name", UpdateIdentityProviderProps{Config: &config, JITProvisioning: new(false)}},
		{"no config", UpdateIdentityProviderProps{Name: new("SSO"), JITProvisioning: new(false)}},
		{"no JIT provisioning", UpdateIdentityProviderProps{Name: new("SSO"), Config: &config}},
		{"invalid config", UpdateIdentityProviderProps{Name: new("SSO"), Config: &badConfig, JITProvisioning: new(false)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := idpNew(t, idpProps())
			before := p.Snapshot()

			assert.ErrorIs(t, p.Configure(tt.props), ErrInvalid)
			assert.Equal(t, before, p.Snapshot(), "nothing changes")
			assert.False(t, p.ManagedByConfiguration())
		})
	}
}

func TestIdentityProviderUnconfigure(t *testing.T) {
	clock.Fix(t, t0)
	p := idpManaged(t)
	clock.Fix(t, t1)

	require.NoError(t, p.Unconfigure())

	assert.Equal(t, IdentityProviderDisabled, p.Status())
	assert.True(t, p.ManagedByConfiguration(), "it stays managed by configuration")
	assert.Equal(t, t1, p.UpdatedAt())
	assert.ErrorIs(t, p.Enable(), ErrManagedByConfiguration, "only the configuration can enable it again")
}

func TestIdentityProviderUnconfigureIsRefusedWhenNotManaged(t *testing.T) {
	p := idpNew(t, idpProps())

	assert.ErrorIs(t, p.Unconfigure(), ErrInvalid)
	assert.True(t, p.Active())
}
