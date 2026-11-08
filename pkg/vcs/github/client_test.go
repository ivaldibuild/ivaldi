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

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	client, err := New(Config{AppID: 7, PrivateKeyPEM: pemBytes, BaseURL: server.URL})
	require.NoError(t, err)
	return client
}

func endpoint() vcs.Endpoint {
	return vcs.Endpoint{
		Provider:       vcs.ProviderGitHub,
		HostURL:        "https://github.com",
		InstallationID: "42",
	}
}

func TestRepositoryAndCommitsBetween(t *testing.T) {
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42/access_tokens" {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			assert.Equal(t, http.MethodPost, r.Method)
			tokenCalls++
			_, _ = w.Write([]byte(`{"token":"test-token","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/repositories/99":
			_, _ = w.Write([]byte(`{
				"id": 99, "full_name": "acme/api", "default_branch": "main",
				"visibility": "private", "clone_url": "https://github.com/acme/api.git",
				"html_url": "https://github.com/acme/api"
			}`))
		case "/repos/acme/api/compare/aaa...bbb":
			_, _ = w.Write([]byte(`{
				"status": "ahead",
				"commits": [
					{"sha":"bbb","html_url":"https://github.com/acme/api/commit/bbb",
					 "commit":{"message":"Add checkout","author":{"name":"Ada","email":"ada@example.com","date":"2026-11-08T10:00:00Z"}}}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	ctx := context.Background()

	repo, err := client.Repository(ctx, endpoint(), "99")
	require.NoError(t, err)
	assert.Equal(t, "acme/api", repo.FullName)
	assert.Equal(t, "private", repo.Visibility)
	assert.Equal(t, "99", repo.ExternalID)

	commits, err := client.CommitsBetween(ctx, endpoint(), repo, "aaa", "bbb")
	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, "bbb", commits[0].SHA)
	assert.Equal(t, "Add checkout", commits[0].Message)
	assert.Equal(t, "Ada", commits[0].AuthorName)
	assert.Equal(t, time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC), commits[0].CommittedAt)
	assert.Equal(t, 1, tokenCalls, "the installation token is cached")
}

func TestRepositoryReportsANotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/42/access_tokens" {
			_, _ = w.Write([]byte(`{"token":"test-token"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := testClient(t, server).Repository(context.Background(), endpoint(), "missing")
	assert.ErrorIs(t, err, vcs.ErrNotFound)
}
