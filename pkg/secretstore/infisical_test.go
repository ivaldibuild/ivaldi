package secretstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// infisicalServer is an Infisical project "prj" whose environment "prod"
// holds the fixture secrets in /app, "nested" in /app/db, and "imported",
// which /app imports. It counts the requests each endpoint gets.
type infisicalServer struct {
	mode     mode
	requests map[string]int
}

func (f *infisicalServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests[r.URL.Path]++
	q := r.URL.Query()
	switch {
	case f.mode == denied || r.Header.Get("Authorization") != "Bearer inf.token":
		w.WriteHeader(http.StatusForbidden)
		return
	case q.Get("projectId") != "prj" || q.Get("environment") != "prod" || q.Get("viewSecretValue") != "true":
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	folders := map[string]map[string]map[string]any{
		"/app": {
			"db":    {"secretKey": "db", "secretValue": dbJSON, "version": 2},
			"plain": {"secretKey": "plain", "secretValue": "hunter22", "version": 1},
		},
		"/app/db": {"nested": {"secretKey": "nested", "secretValue": "deep", "version": 4}},
	}
	folder := folders[q.Get("secretPath")]
	if r.URL.Path == "/api/v4/secrets" {
		list := []map[string]any{}
		for _, s := range folder {
			list = append(list, s)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": list, "imports": []any{}})
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/v4/secrets/")
	secret, ok := folder[name]
	if !ok && name == "imported" && q.Get("secretPath") == "/app" && q.Get("includeImports") == "true" {
		secret, ok = map[string]any{"secretKey": "imported", "secretValue": "from-elsewhere", "version": 3}, true
	}
	if !ok || (q.Get("version") != "" && q.Get("version") != fmt.Sprint(secret["version"])) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"secret": secret})
}

func newInfisicalFixture(t *testing.T, m mode) (Store, *infisicalServer) {
	fake := &infisicalServer{mode: m, requests: map[string]int{}}
	server := httptest.NewServer(fake)
	if m == unreachable {
		server.Close()
	} else {
		t.Cleanup(server.Close)
	}
	return NewInfisical(InfisicalConfig{
		SiteURL: server.URL, ProjectID: "prj", Environment: "prod", SecretPath: "/app",
		Token: func(context.Context) (string, error) { return "inf.token", nil },
	}), fake
}

func infisicalFixture(t *testing.T, m mode) Store {
	store, _ := newInfisicalFixture(t, m)
	return store
}

// A path is relative to the store's folder and cannot leave it. Several
// latest values in one folder are read with one list; a pinned version, or
// a secret the folder imports, is read by name.
func TestInfisical_Folders(t *testing.T) {
	store, fake := newInfisicalFixture(t, healthy)
	ctx := t.Context()

	v, err := store.Get(ctx, Ref{Path: "db/nested"})
	require.NoError(t, err)
	assert.Equal(t, Value{Value: "deep", Version: "4", Source: Ref{Path: "db/nested"}}, v)

	_, err = store.Get(ctx, Ref{Path: "../other/db"})
	assert.ErrorIs(t, err, ErrNotFound)

	values, err := GetMany(ctx, store, []Ref{{Path: "db", Field: "port"}, {Path: "plain"}, {Path: "imported"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"5432", "hunter22", "from-elsewhere"}, []string{values[0].Value, values[1].Value, values[2].Value})
	assert.Equal(t, 1, fake.requests["/api/v4/secrets"], "the folder is listed once")
	assert.Equal(t, 1, fake.requests["/api/v4/secrets/imported"], "an imported secret is read by name")

	values, err = GetMany(ctx, store, []Ref{{Path: "db", Field: "password", Version: "2"}, {Path: "plain"}})
	require.NoError(t, err)
	assert.Equal(t, "2", values[0].Version)
	assert.Equal(t, 1, fake.requests["/api/v4/secrets"], "a pinned version is never listed")

	_, err = store.Get(ctx, Ref{Path: "plain", Version: "latest"})
	assert.ErrorIs(t, err, ErrVersionGone)
}

// Each JWT login posts the identity and the JWT to its auth method; a
// refusal is the identity's, not a failure worth retrying.
func TestInfisicalLogins(t *testing.T) {
	var path string
	var sent map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, sent = r.URL.Path, nil
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		if sent["identityId"] == "other" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "inf.token", "expiresIn": 7200, "tokenType": "Bearer"})
	}))
	t.Cleanup(server.Close)
	ctx := context.Background()

	for _, method := range []InfisicalMethod{InfisicalOIDC, InfisicalJWT, InfisicalKubernetes} {
		token, err := InfisicalLogin(ctx, nil, server.URL, method, "idt_1", "eyJ.jwt")
		require.NoError(t, err)
		assert.Equal(t, "inf.token", token)
		assert.Equal(t, "/api/v1/auth/"+string(method)+"-auth/login", path)
		assert.Equal(t, map[string]string{"identityId": "idt_1", "jwt": "eyJ.jwt"}, sent)
	}

	token, err := InfisicalUniversalAuthLogin(ctx, nil, server.URL, "client", "secret")
	require.NoError(t, err)
	assert.Equal(t, "inf.token", token)
	assert.Equal(t, "/api/v1/auth/universal-auth/login", path)
	assert.Equal(t, map[string]string{"clientId": "client", "clientSecret": "secret"}, sent)

	_, err = InfisicalLogin(ctx, nil, server.URL, InfisicalOIDC, "other", "eyJ.jwt")
	require.ErrorIs(t, err, ErrDenied)
	assert.False(t, Transient(err))
}
