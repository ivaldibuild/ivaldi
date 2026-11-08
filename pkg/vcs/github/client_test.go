package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const tokenPath = "/app/installations/42/access_tokens"

// testClient serves both GitHub's web and API addresses from server.
func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	client, err := New(Config{
		AppID: 7, PrivateKeyPEM: pemBytes, Slug: "ivaldi", ClientID: "Iv1.test", ClientSecret: "secret",
		WebURL: server.URL, APIURL: server.URL,
	})
	require.NoError(t, err)
	return client
}

func endpoint(server *httptest.Server) vcs.Endpoint {
	return vcs.Endpoint{Provider: vcs.ProviderGitHub, HostURL: server.URL, InstallationID: "42"}
}

func writeToken(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"token":"test-token","expires_at":"2099-01-01T00:00:00Z"}`))
}

func TestRepositoryAndCommitsBetween(t *testing.T) {
	var tokenCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tokenPath {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case tokenPath:
			assert.Equal(t, http.MethodPost, r.Method)
			tokenCalls++
			writeToken(w)
		case "/repositories/99":
			_, _ = w.Write([]byte(`{
				"id": 99, "full_name": "acme/api", "default_branch": "main",
				"visibility": "private", "clone_url": "https://github.com/acme/api.git",
				"html_url": "https://github.com/acme/api"
			}`))
		case "/repositories/99/compare/aaa...bbb":
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", `<`+server.URL+`/repositories/99/compare/aaa...bbb?per_page=100&page=2>; rel="next"`)
				_, _ = w.Write([]byte(`{"commits": [
					{"sha":"abc","commit":{"message":"First","author":{"name":"Ada","email":"ada@example.com","date":"2026-11-08T09:00:00Z"}}}
				]}`))
				return
			}
			_, _ = w.Write([]byte(`{"commits": [
				{"sha":"bbb","html_url":"https://github.com/acme/api/commit/bbb",
				 "commit":{"message":"Add checkout","author":{"name":"Ada","email":"ada@example.com","date":"2026-11-08T10:00:00Z"}}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	ctx := context.Background()

	repo, err := client.Repository(ctx, endpoint(server), "99")
	require.NoError(t, err)
	assert.Equal(t, "acme/api", repo.FullName)
	assert.Equal(t, "private", repo.Visibility)
	assert.Equal(t, "99", repo.ExternalID)

	commits, err := client.CommitsBetween(ctx, endpoint(server), repo, "aaa", "bbb")
	require.NoError(t, err)
	require.Len(t, commits, 2, "both pages are read")
	assert.Equal(t, "abc", commits[0].SHA)
	assert.Equal(t, "bbb", commits[1].SHA)
	assert.Equal(t, "Add checkout", commits[1].Message)
	assert.Equal(t, time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC), commits[1].CommittedAt)
	assert.Equal(t, 1, tokenCalls, "the installation token is cached")
}

func TestRepositoryReportsANotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := testClient(t, server).Repository(context.Background(), endpoint(server), "missing")
	assert.ErrorIs(t, err, vcs.ErrNotFound)
}

func TestARateLimitIsUnavailableNotUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w)
			return
		}
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := testClient(t, server).Repository(context.Background(), endpoint(server), "99")
	assert.ErrorIs(t, err, vcs.ErrUnavailable)
}

// The App's JWT and tokens must never go to a host a Connection names
// unless it is the configured GitHub.
func TestTheClientContactsOnlyTheConfiguredHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
	}))
	defer server.Close()
	client := testClient(t, server)

	for name, at := range map[string]vcs.Endpoint{
		"another host":     {Provider: vcs.ProviderGitHub, HostURL: "https://evil.example", InstallationID: "42"},
		"github.com":       {Provider: vcs.ProviderGitHub, HostURL: "https://github.com", InstallationID: "42"},
		"another provider": {Provider: vcs.ProviderGitLab, HostURL: server.URL, InstallationID: "42"},
		"unparseable host": {Provider: vcs.ProviderGitHub, HostURL: "://", InstallationID: "42"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.Repository(context.Background(), at, "99")
			assert.ErrorIs(t, err, vcs.ErrUnsupportedProvider)
		})
	}
}

func TestNewDerivesTheAPIAddress(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for web, api := range map[string]string{
		"":                        "https://api.github.com",
		"https://github.com/":     "https://api.github.com",
		"https://ghe.example.com": "https://ghe.example.com/api/v3",
	} {
		client, err := New(Config{AppID: 7, PrivateKeyPEM: pemBytes, Slug: "ivaldi", ClientID: "id", ClientSecret: "s", WebURL: web})
		require.NoError(t, err)
		assert.Equal(t, api, client.apiURL, web)
	}
}

func installServer(t *testing.T, installations string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			require.NoError(t, r.ParseForm())
			if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("client_secret") != "secret" {
				_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"user-token","token_type":"bearer"}`))
		case "/user/installations":
			assert.Equal(t, "Bearer user-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(installations))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestVerifyInstallation(t *testing.T) {
	server := installServer(t, `{"installations":[
		{"id": 41, "app_id": 7, "account": {"login": "other"}},
		{"id": 42, "app_id": 7, "account": {"login": "acme"}},
		{"id": 43, "app_id": 8, "account": {"login": "another-app"}}
	]}`)
	defer server.Close()
	client := testClient(t, server)
	ctx := context.Background()

	inst, err := client.VerifyInstallation(ctx, "good-code", "42")
	require.NoError(t, err)
	assert.Equal(t, vcs.Installation{
		Provider: vcs.ProviderGitHub, HostURL: server.URL, InstallationID: "42", AccountName: "acme",
	}, inst)

	t.Run("an installation the person cannot access", func(t *testing.T) {
		_, err := client.VerifyInstallation(ctx, "good-code", "99")
		assert.ErrorIs(t, err, vcs.ErrUnauthorized)
	})
	t.Run("another App's installation", func(t *testing.T) {
		_, err := client.VerifyInstallation(ctx, "good-code", "43")
		assert.ErrorIs(t, err, vcs.ErrUnauthorized)
	})
	t.Run("a refused code", func(t *testing.T) {
		_, err := client.VerifyInstallation(ctx, "stolen-code", "42")
		assert.ErrorIs(t, err, vcs.ErrUnauthorized)
	})
}

func TestInstallURL(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	assert.Equal(t, server.URL+"/github-apps/ivaldi/installations/new?state=s%2B1", testClient(t, server).InstallURL("s+1"))
}

func TestRepositoriesListsWhatTheInstallationCanRead(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == tokenPath:
			writeToken(w)
		case r.URL.Path == "/installation/repositories" && r.URL.Query().Get("page") == "":
			w.Header().Set("Link", `<`+server.URL+`/installation/repositories?per_page=100&page=2>; rel="next"`)
			_, _ = w.Write([]byte(`{"total_count": 2, "repositories": [{"id": 1, "full_name": "acme/web", "visibility": "public"}]}`))
		case r.URL.Path == "/installation/repositories":
			_, _ = w.Write([]byte(`{"total_count": 2, "repositories": [{"id": 2, "full_name": "acme/api", "visibility": "private"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repositories, err := testClient(t, server).Repositories(context.Background(), endpoint(server))
	require.NoError(t, err)
	require.Len(t, repositories, 2)
	assert.Equal(t, "1", repositories[0].ExternalID)
	assert.Equal(t, "acme/api", repositories[1].FullName)
}
