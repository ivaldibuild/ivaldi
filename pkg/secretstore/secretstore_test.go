package secretstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every store holds the same two secrets: "db", a JSON object at version
// 2, and "plain", a single plain value at version 1.
const dbJSON = `{"password":"s3cret","port":5432}`

// mode is how a fixture store behaves.
type mode int

const (
	healthy mode = iota
	denied
	unreachable
)

type provider struct {
	name string
	new  func(t *testing.T, m mode) Store
	// plain is the reference to the plain value: stores of maps name no
	// field when the map has one key.
	plain Ref
}

func providers() []provider {
	return []provider{
		{name: "kubernetes", new: kubernetesFixture, plain: Ref{Path: "plain"}},
		{name: "aws_secrets_manager", new: awsFixture, plain: Ref{Path: "plain"}},
		{name: "vault", new: vaultFixture, plain: Ref{Path: "plain"}},
		{name: "doppler", new: dopplerFixture, plain: Ref{Path: "plain"}},
	}
}

func TestStores_Contract(t *testing.T) {
	for _, p := range providers() {
		t.Run(p.name, func(t *testing.T) {
			ctx := t.Context()
			store := p.new(t, healthy)

			v, err := store.Get(ctx, Ref{Path: "db", Field: "password"})
			require.NoError(t, err)
			assert.Equal(t, "s3cret", v.Value)
			if store.Versions() {
				assert.Equal(t, "2", v.Version)
			}

			v, err = store.Get(ctx, p.plain)
			require.NoError(t, err)
			assert.Equal(t, "hunter22", v.Value)

			values, err := GetMany(ctx, store, []Ref{{Path: "db", Field: "password"}, p.plain})
			require.NoError(t, err)
			assert.Equal(t, []string{"s3cret", "hunter22"}, []string{values[0].Value, values[1].Value})

			_, err = store.Get(ctx, Ref{Path: "missing"})
			assert.ErrorIs(t, err, ErrNotFound)

			_, err = store.Get(ctx, Ref{Path: "db", Field: "nope"})
			assert.ErrorIs(t, err, ErrFieldMissing)

			_, err = store.Get(ctx, Ref{Path: "db", Field: "password", Version: "99"})
			assert.ErrorIs(t, err, ErrVersionGone)

			_, err = p.new(t, denied).Get(ctx, Ref{Path: "db", Field: "password"})
			assert.ErrorIs(t, err, ErrDenied)

			_, err = p.new(t, unreachable).Get(ctx, Ref{Path: "db", Field: "password"})
			assert.True(t, Transient(err), "unreachable is transient: %v", err)
		})
	}
}

func TestErrors_NeverContainTheValue(t *testing.T) {
	for _, p := range providers() {
		_, err := p.new(t, healthy).Get(t.Context(), Ref{Path: "db", Field: "nope"})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "s3cret", p.name)
	}
}

func kubernetesFixture(_ *testing.T, m mode) Store {
	secrets := map[string]map[string][]byte{
		"db":    {"password": []byte("s3cret"), "port": []byte("5432")},
		"plain": {"value": []byte("hunter22")},
	}
	versions := map[string]string{"db": "2", "plain": "1"}
	return NewKubernetes(func(_ context.Context, name string) (map[string][]byte, string, error) {
		switch m {
		case denied:
			return nil, "", ErrDenied
		case unreachable:
			return nil, "", ErrUnavailable
		}
		data, ok := secrets[name]
		if !ok {
			return nil, "", ErrNotFound
		}
		return data, versions[name], nil
	})
}

// fakeSecretsManager serves the fixture secrets with Secrets Manager's
// errors.
type fakeSecretsManager struct {
	mode mode
	// batchErrors fails these secrets inside a batch's 200, by error code.
	batchErrors map[string]string
	// last is the last single read asked for.
	last *secretsmanager.GetSecretValueInput
}

func (f *fakeSecretsManager) err() error {
	switch f.mode {
	case denied:
		return &smithy.GenericAPIError{Code: "AccessDeniedException", Fault: smithy.FaultClient}
	case unreachable:
		return &smithy.GenericAPIError{Code: "InternalServiceError", Fault: smithy.FaultServer}
	}
	return nil
}

func (f *fakeSecretsManager) lookup(id string, version *string) (*string, *string, error) {
	if err := f.err(); err != nil {
		return nil, nil, err
	}
	values := map[string]string{"db": dbJSON, "plain": "hunter22"}
	versions := map[string]string{"db": "2", "plain": "1"}
	v, ok := values[id]
	if !ok || (version != nil && *version != versions[id]) {
		return nil, nil, &types.ResourceNotFoundException{Message: aws.String("not found")}
	}
	return aws.String(v), aws.String(versions[id]), nil
}

func (f *fakeSecretsManager) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.last = in
	version := in.VersionStage
	if in.VersionId != nil {
		version = in.VersionId
	}
	value, id, err := f.lookup(aws.ToString(in.SecretId), version)
	if err != nil {
		return nil, err
	}
	return &secretsmanager.GetSecretValueOutput{Name: in.SecretId, SecretString: value, VersionId: id}, nil
}

func (f *fakeSecretsManager) BatchGetSecretValue(_ context.Context, in *secretsmanager.BatchGetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.BatchGetSecretValueOutput, error) {
	out := &secretsmanager.BatchGetSecretValueOutput{}
	for _, id := range in.SecretIdList {
		if code, ok := f.batchErrors[id]; ok {
			out.Errors = append(out.Errors, types.APIErrorType{SecretId: aws.String(id), ErrorCode: aws.String(code)})
			continue
		}
		value, version, err := f.lookup(id, nil)
		var missing *types.ResourceNotFoundException
		if err != nil && !errors.As(err, &missing) {
			return nil, err
		}
		if err == nil {
			out.SecretValues = append(out.SecretValues, types.SecretValueEntry{Name: aws.String(id), SecretString: value, VersionId: version})
		}
	}
	return out, nil
}

func awsFixture(_ *testing.T, m mode) Store { return NewAWS(&fakeSecretsManager{mode: m}) }

func TestAWS_ABatchsPerSecretErrorsAreMappedLikeSingleReads(t *testing.T) {
	for code, want := range map[string]error{
		"InternalServiceError":  ErrUnavailable,
		"AccessDeniedException": ErrDenied,
		"DecryptionFailure":     ErrDenied,
	} {
		store := NewAWS(&fakeSecretsManager{batchErrors: map[string]string{"db": code}})
		_, err := GetMany(t.Context(), store, []Ref{{Path: "plain"}, {Path: "db", Field: "password"}})
		assert.ErrorIs(t, err, want, code)
		assert.NotErrorIs(t, err, ErrNotFound, code)
	}
}

func TestAWS_VersionIdsAndStagesAreSentAsSuch(t *testing.T) {
	tests := []struct {
		version   string
		id, stage string
	}{
		{version: "0123456789abcdef0123456789abcdef", id: "0123456789abcdef0123456789abcdef"},
		{version: "01234567-89ab-cdef-0123-456789abcdef", id: "01234567-89ab-cdef-0123-456789abcdef"},
		{version: "AWSPREVIOUS", stage: "AWSPREVIOUS"},
		{version: "stage:release-2026", stage: "release-2026"},
	}
	for _, tt := range tests {
		api := &fakeSecretsManager{}
		_, _ = NewAWS(api).Get(t.Context(), Ref{Path: "db", Version: tt.version})
		require.NotNil(t, api.last, tt.version)
		assert.Equal(t, tt.id, aws.ToString(api.last.VersionId), tt.version)
		assert.Equal(t, tt.stage, aws.ToString(api.last.VersionStage), tt.version)
	}
}

// vaultFixture is a Vault with Kubernetes auth and a KV v2 mount "kv".
func vaultFixture(t *testing.T, m mode) Store {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/kubernetes/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if m == denied || body["jwt"] != "sa-token" || body["role"] != "ivaldi" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"auth":{"client_token":"hvs.token"}}`))
		case r.Header.Get("X-Vault-Token") != "hvs.token":
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/v1/kv/data/db" && (r.URL.Query().Get("version") == "" || r.URL.Query().Get("version") == "2"):
			_, _ = w.Write([]byte(`{"data":{"data":` + dbJSON + `,"metadata":{"version":2}}}`))
		case r.URL.Path == "/v1/kv/data/plain" && r.URL.Query().Get("version") == "":
			_, _ = w.Write([]byte(`{"data":{"data":{"value":"hunter22"},"metadata":{"version":1}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	if m == unreachable {
		server.Close()
	} else {
		t.Cleanup(server.Close)
	}
	return NewVault(VaultConfig{
		Address: server.URL, Mount: "kv", AuthRole: "ivaldi",
		JWT: func() (string, error) { return "sa-token", nil },
	})
}

func dopplerFixture(t *testing.T, m mode) Store {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m == denied || r.Header.Get("Authorization") != "Bearer dp.st.token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"db": dbJSON, "plain": "hunter22"})
	}))
	if m == unreachable {
		server.Close()
	} else {
		t.Cleanup(server.Close)
	}
	return NewDoppler(DopplerConfig{
		API:   server.URL,
		Token: func(context.Context) (string, error) { return "dp.st.token", nil },
	})
}

// A token that is not a service token names the config it reads.
func TestDoppler_NamesItsConfig(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]string{"plain": "hunter22"})
	}))
	t.Cleanup(server.Close)
	store := NewDoppler(DopplerConfig{
		API: server.URL, Project: "payments", Config: "prd",
		Token: func(context.Context) (string, error) { return "dp.sa.token", nil },
	})

	_, err := store.Get(context.Background(), Ref{Path: "plain"})
	require.NoError(t, err)
	assert.Equal(t, url.Values{"format": {"json"}, "project": {"payments"}, "config": {"prd"}}, query)
}

// A login carries the service-account token, so it goes only to the address
// the store names, wherever that server's reply points.
func TestVault_DoesNotFollowARedirect(t *testing.T) {
	var elsewhere int
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere++ }))
	t.Cleanup(other.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	store := NewVault(VaultConfig{
		Address: server.URL, Mount: "kv", AuthRole: "ivaldi",
		JWT: func() (string, error) { return "sa-token", nil },
	})

	_, err := store.Get(context.Background(), Ref{Path: "api"})
	require.Error(t, err)
	assert.Zero(t, elsewhere)
}
