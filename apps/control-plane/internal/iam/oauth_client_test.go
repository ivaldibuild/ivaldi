package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const clientMetadataURL = "https://app.example.com/oauth/client.json"

func clientDynamicProps() NewOAuthClientProps {
	return NewOAuthClientProps{
		Registration: RegistrationDynamic,
		Name:         "Editor",
		RedirectURIs: []string{"https://app.example.com/callback", "http://127.0.0.1:8080/cb"},
	}
}

func clientMetadataProps() NewOAuthClientProps {
	return NewOAuthClientProps{
		Registration: RegistrationMetadataDocument,
		Name:         "Editor",
		RedirectURIs: []string{"https://app.example.com/callback"},
		MetadataURL:  new(clientMetadataURL),
	}
}

func TestNewOAuthClientDynamic(t *testing.T) {
	clock.Fix(t, t0)

	c, err := NewOAuthClient(clientDynamicProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("ocl", c.ID()), "a new client gets an ocl_ ID: %q", c.ID())
	assert.Equal(t, c.ID(), c.ClientID(), "a dynamic client's client_id defaults to its ID")
	assert.Equal(t, RegistrationDynamic, c.Registration())
	assert.Equal(t, "Editor", c.Name())
	assert.Equal(t, []string{"https://app.example.com/callback", "http://127.0.0.1:8080/cb"}, c.RedirectURIs())
	assert.Nil(t, c.MetadataURL())
	assert.Equal(t, t0, c.CreatedAt())
	assert.Nil(t, c.LastUsedAt())
}

func TestNewOAuthClientDynamicKeepsGivenClientID(t *testing.T) {
	props := clientDynamicProps()
	props.ClientID = "custom-client"

	c, err := NewOAuthClient(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "custom-client", c.ClientID())
}

func TestNewOAuthClientFirstParty(t *testing.T) {
	c, err := NewOAuthClient(NewOAuthClientProps{
		ClientID:     "ivaldictl",
		Registration: RegistrationFirstParty,
		Name:         "Ivaldi CLI",
		RedirectURIs: []string{"http://127.0.0.1/callback"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "ivaldictl", c.ClientID())
	assert.True(t, c.FirstParty())
	assert.True(t, c.AllowsRedirect("http://127.0.0.1:53123/callback"), "a loopback redirect may use any port")
}

func TestNewOAuthClientFirstPartyIsRefused(t *testing.T) {
	t.Run("without a client ID", func(t *testing.T) {
		_, err := NewOAuthClient(NewOAuthClientProps{Registration: RegistrationFirstParty, Name: "CLI", RedirectURIs: []string{"http://127.0.0.1/cb"}}, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("with a metadata URL", func(t *testing.T) {
		_, err := NewOAuthClient(NewOAuthClientProps{ClientID: "x", Registration: RegistrationFirstParty, Name: "CLI", RedirectURIs: []string{"http://127.0.0.1/cb"}, MetadataURL: new(clientMetadataURL)}, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
}

func TestOAuthClientFirstParty(t *testing.T) {
	dynamic, err := NewOAuthClient(clientDynamicProps(), nil)
	require.NoError(t, err)
	assert.False(t, dynamic.FirstParty())
}

func TestNewOAuthClientMetadataDocument(t *testing.T) {
	c, err := NewOAuthClient(clientMetadataProps(), nil)

	require.NoError(t, err)
	assert.Equal(t, clientMetadataURL, c.ClientID(), "a metadata document client's client_id defaults to its metadata URL")
	require.NotNil(t, c.MetadataURL())
	assert.Equal(t, clientMetadataURL, *c.MetadataURL())

	props := clientMetadataProps()
	props.ClientID = clientMetadataURL
	_, err = NewOAuthClient(props, nil)
	assert.NoError(t, err, "giving the metadata URL as client ID is allowed")
}

func TestNewOAuthClientWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("ocl")
	props := clientDynamicProps()
	props.ClientID = given
	props.CreatedAt = new(t0)
	props.LastUsedAt = new(t1)

	c, err := NewOAuthClient(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, c.ID())
	assert.Equal(t, t0, c.CreatedAt())
	require.NotNil(t, c.LastUsedAt())
	assert.Equal(t, t1, *c.LastUsedAt())

	*props.LastUsedAt = t2
	assert.Equal(t, t1, *c.LastUsedAt(), "the given time is copied")
}

func TestNewOAuthClientIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		props  func() NewOAuthClientProps
		mutate func(*NewOAuthClientProps)
		id     *string
	}{
		{"no name", clientDynamicProps, func(p *NewOAuthClientProps) { p.Name = "" }, nil},
		{"blank name", clientDynamicProps, func(p *NewOAuthClientProps) { p.Name = "  " }, nil},
		{"name too long", clientDynamicProps, func(p *NewOAuthClientProps) { p.Name = strings.Repeat("é", maxNameLength+1) }, nil},
		{"no redirect URIs", clientDynamicProps, func(p *NewOAuthClientProps) { p.RedirectURIs = nil }, nil},
		{"too many redirect URIs", clientDynamicProps, func(p *NewOAuthClientProps) {
			p.RedirectURIs = nil
			for i := range maxRedirectURIs + 1 {
				p.RedirectURIs = append(p.RedirectURIs, "https://app.example.com/cb"+strings.Repeat("x", i))
			}
		}, nil},
		{"unknown registration", clientDynamicProps, func(p *NewOAuthClientProps) { p.Registration = "static" }, nil},
		{"empty registration", clientDynamicProps, func(p *NewOAuthClientProps) { p.Registration = "" }, nil},
		{"dynamic with metadata URL", clientDynamicProps, func(p *NewOAuthClientProps) { p.MetadataURL = new(clientMetadataURL) }, nil},
		{"metadata document without URL", clientMetadataProps, func(p *NewOAuthClientProps) { p.MetadataURL = nil }, nil},
		{"metadata document with empty URL", clientMetadataProps, func(p *NewOAuthClientProps) { p.MetadataURL = new("") }, nil},
		{"metadata URL over http", clientMetadataProps, func(p *NewOAuthClientProps) { p.MetadataURL = new("http://app.example.com/client.json") }, nil},
		{"metadata client ID differs from URL", clientMetadataProps, func(p *NewOAuthClientProps) { p.ClientID = "other" }, nil},
		{"client ID too long", clientDynamicProps, func(p *NewOAuthClientProps) { p.ClientID = strings.Repeat("a", maxClientIDLength+1) }, nil},
		{"ID with wrong prefix", clientDynamicProps, func(*NewOAuthClientProps) {}, new(id.New("ort"))},
		{"malformed ID", clientDynamicProps, func(*NewOAuthClientProps) {}, new("ocl_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := tt.props()
			tt.mutate(&props)
			_, err := NewOAuthClient(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestValidateRedirectURIs(t *testing.T) {
	accepted := []string{
		"https://app.example.com/callback",
		"https://app.example.com/callback?x=1",
		"http://127.0.0.1/cb",
		"http://127.0.0.1:53682/cb",
		"http://localhost:3000/cb",
		"http://[::1]:8080/cb",
		"com.example.app:/oauth/done",
		"com.example.app://callback",
	}
	for _, uri := range accepted {
		t.Run("accepts "+uri, func(t *testing.T) {
			assert.NoError(t, validateRedirectURIs([]string{uri}))
		})
	}

	refused := []string{
		"",
		"/relative/callback",
		"https://app.example.com/callback#frag",
		"https://app.example.com/callback#",
		"http://app.example.com/callback",
		"http://192.168.1.10/cb",
		"https:///nohost",
		"myapp:/callback",
		"javascript:alert(1)",
	}
	for _, uri := range refused {
		t.Run("refuses "+uri, func(t *testing.T) {
			assert.ErrorIs(t, validateRedirectURIs([]string{uri}), ErrInvalid)
		})
	}

	t.Run("refuses duplicates", func(t *testing.T) {
		err := validateRedirectURIs([]string{"https://a.example.com/cb", "https://a.example.com/cb"})
		assert.ErrorIs(t, err, ErrInvalid)
	})
}

func TestOAuthClientAllowsRedirect(t *testing.T) {
	props := clientDynamicProps()
	props.RedirectURIs = []string{
		"https://app.example.com/callback",
		"http://127.0.0.1:8080/cb",
		"http://localhost/cb?mode=cli",
		"com.example.app:/done",
	}
	c, err := NewOAuthClient(props, nil)
	require.NoError(t, err)

	tests := []struct {
		uri  string
		want bool
	}{
		{"https://app.example.com/callback", true},
		{"com.example.app:/done", true},
		{"http://127.0.0.1:8080/cb", true},
		{"http://127.0.0.1:53682/cb", true},
		{"http://127.0.0.1/cb", true},
		{"http://localhost:9999/cb?mode=cli", true},
		{"http://localhost:9999/cb", false},
		{"http://127.0.0.1:53682/other", false},
		{"http://localhost:8080/cb", false},
		{"https://127.0.0.1:8080/cb", false},
		{"https://app.example.com:8443/callback", false},
		{"https://app.example.com/callback/", false},
		{"https://evil.example.com/callback", false},
		{"com.example.app:/other", false},
		{"://bad", false},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.want, c.AllowsRedirect(tt.uri))
		})
	}
}

func TestOAuthClientGettersReturnCopies(t *testing.T) {
	props := clientMetadataProps()
	props.LastUsedAt = new(t0)
	c, err := NewOAuthClient(props, nil)
	require.NoError(t, err)

	c.RedirectURIs()[0] = "https://evil.example.com"
	*c.MetadataURL() = "https://evil.example.com"
	*c.LastUsedAt() = t2
	snap := c.Snapshot()
	snap.RedirectURIs[0] = "https://evil.example.com"
	*snap.MetadataURL = "https://evil.example.com"

	props.RedirectURIs[0] = "https://evil.example.com"
	*props.MetadataURL = "https://evil.example.com"

	assert.Equal(t, []string{"https://app.example.com/callback"}, c.RedirectURIs())
	assert.Equal(t, clientMetadataURL, *c.MetadataURL())
	assert.Equal(t, t0, *c.LastUsedAt())
}

func TestOAuthClientSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	c, err := NewOAuthClient(clientMetadataProps(), nil)
	require.NoError(t, err)

	assert.Equal(t, OAuthClientSnapshot{
		ID:           c.ID(),
		ClientID:     clientMetadataURL,
		Registration: RegistrationMetadataDocument,
		Name:         "Editor",
		RedirectURIs: []string{"https://app.example.com/callback"},
		MetadataURL:  new(clientMetadataURL),
		CreatedAt:    t0,
	}, c.Snapshot())
}

func TestOAuthClientUpdate(t *testing.T) {
	c, err := NewOAuthClient(clientDynamicProps(), nil)
	require.NoError(t, err)

	uris := []string{"https://new.example.com/cb"}
	require.NoError(t, c.Update("Renamed", uris))
	uris[0] = "https://evil.example.com"

	assert.Equal(t, "Renamed", c.Name())
	assert.Equal(t, []string{"https://new.example.com/cb"}, c.RedirectURIs(), "the given URIs are copied")
}

func TestOAuthClientUpdateIsRefused(t *testing.T) {
	tests := []struct {
		name string
		uris []string
		cn   string
	}{
		{"no name", []string{"https://new.example.com/cb"}, ""},
		{"no redirect URIs", nil, "Renamed"},
		{"invalid redirect URI", []string{"http://example.com/cb"}, "Renamed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewOAuthClient(clientDynamicProps(), nil)
			require.NoError(t, err)
			before := c.Snapshot()

			assert.ErrorIs(t, c.Update(tt.cn, tt.uris), ErrInvalid)
			assert.Equal(t, before, c.Snapshot(), "nothing changes")
		})
	}
}

func TestOAuthClientRecordUse(t *testing.T) {
	clock.Fix(t, t0)
	c, err := NewOAuthClient(clientDynamicProps(), nil)
	require.NoError(t, err)

	clock.Fix(t, t1)
	c.RecordUse()

	require.NotNil(t, c.LastUsedAt())
	assert.Equal(t, t1, *c.LastUsedAt())
}
