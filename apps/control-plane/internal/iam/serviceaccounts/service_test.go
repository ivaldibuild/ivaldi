package serviceaccounts

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const org = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"

var (
	t0  = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	ada = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: identity.ActorUser}
	ctx = context.Background()
)

// store fakes both repositories and the transactor, keeping objects as they
// are written, and the recorders through auditLog and outboxLog.
type store struct {
	accounts map[string]*iam.ServiceAccount
	tokens   map[string]*iam.APIToken
	audit    []audit.Event
	events   []events.Envelope
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	accounts, tokens, a, e := maps.Clone(s.accounts), maps.Clone(s.tokens), len(s.audit), len(s.events)
	if err := fn(ctx); err != nil {
		s.accounts, s.tokens, s.audit, s.events = accounts, tokens, s.audit[:a], s.events[:e]
		return err
	}
	return nil
}

type accounts struct{ *store }

func (r accounts) Write(_ context.Context, sa *iam.ServiceAccount) error {
	r.store.accounts[sa.ID()] = sa
	return nil
}

func (r accounts) Get(_ context.Context, organizationID, id string) (*iam.ServiceAccount, error) {
	sa, ok := r.store.accounts[id]
	if !ok || sa.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return sa, nil
}

func (r accounts) List(_ context.Context, organizationID string, includeInactive bool) ([]*iam.ServiceAccount, error) {
	var out []*iam.ServiceAccount
	for _, sa := range r.store.accounts {
		if sa.OrganizationID() == organizationID && (includeInactive || sa.Active()) {
			out = append(out, sa)
		}
	}
	return out, nil
}

type tokens struct{ *store }

func (r tokens) Write(_ context.Context, t *iam.APIToken) error {
	r.store.tokens[t.ID()] = t
	return nil
}

func (r tokens) Get(_ context.Context, organizationID, id string) (*iam.APIToken, error) {
	t, ok := r.store.tokens[id]
	if !ok || t.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return t, nil
}

func (r tokens) GetByHash(context.Context, []byte) (*iam.APIToken, error) {
	return nil, iam.ErrNotFound
}

func (r tokens) ListByPrincipal(_ context.Context, organizationID, principalID string) ([]*iam.APIToken, error) {
	var out []*iam.APIToken
	for _, t := range r.store.tokens {
		if t.OrganizationID() == organizationID && t.PrincipalID() == principalID {
			out = append(out, t)
		}
	}
	return out, nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.events = append(o.s.events, envs...)
	return nil
}

type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome, d.Reason = authorization.OutcomeDeny, "denied by the test"
	}
	return d, nil
}

type world struct {
	*store
	svc   iam.ServiceAccountService
	authz *authorizer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := &store{accounts: map[string]*iam.ServiceAccount{}, tokens: map[string]*iam.APIToken{}}
	authz := &authorizer{}
	svc, err := NewService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz}
}

func (s *store) options(authz authorization.Authorizer) []ServiceOption {
	return []ServiceOption{
		WithServiceAccountRepo(accounts{s}), WithAPITokenRepo(tokens{s}), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

func TestNewServiceRequiresItsDependencies(t *testing.T) {
	w := newWorld(t)
	all := w.options(w.authz)
	for i := range all {
		without := append(append([]ServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

func TestServiceAccountsAndTheirTokens(t *testing.T) {
	w := newWorld(t)
	sa, err := w.svc.CreateServiceAccount(ctx, ada, iam.CreateServiceAccountInput{OrganizationID: org, DisplayName: "GitHub Actions", Description: "Releases from CI"})
	require.NoError(t, err)
	assert.Equal(t, iam.PrincipalServiceAccount, sa.Kind())
	assert.Equal(t, ada.PrincipalID, sa.CreatedByPrincipalID())
	req := w.authz.asked[0]
	assert.Equal(t, iam.PermissionServiceAccountManage, req.Permission)
	assert.Equal(t, org, req.ResourceID, "checked on the Organization")

	expires := t0.Add(90 * 24 * time.Hour)
	tok, secret, err := w.svc.CreateAPIToken(ctx, ada, iam.CreateAPITokenInput{OrganizationID: org, ServiceAccountID: sa.ID(), Name: "github-actions", ExpiresAt: &expires})
	require.NoError(t, err)
	assert.True(t, iam.IsAPIToken(secret))
	assert.True(t, strings.HasPrefix(secret, tok.TokenPrefix()), "the stored prefix tells it apart")
	assert.Equal(t, iam.HashAPIToken(secret), tok.TokenHash(), "only the hash is kept")
	assert.Equal(t, &expires, tok.ExpiresAt())

	listed, err := w.svc.ListAPITokens(ctx, ada, org, sa.ID())
	require.NoError(t, err)
	assert.Len(t, listed, 1)

	revoked, err := w.svc.RevokeAPIToken(ctx, ada, org, tok.ID())
	require.NoError(t, err)
	assert.True(t, revoked.Revoked())
	_, err = w.svc.RevokeAPIToken(ctx, ada, org, tok.ID())
	require.NoError(t, err, "revoking again does nothing")

	_, err = w.svc.SuspendServiceAccount(ctx, ada, org, sa.ID())
	require.NoError(t, err)
	_, _, err = w.svc.CreateAPIToken(ctx, ada, iam.CreateAPITokenInput{OrganizationID: org, ServiceAccountID: sa.ID(), Name: "again"})
	assert.ErrorIs(t, err, iam.ErrStatus, "a suspended service account gets no tokens")
	active, err := w.svc.ListServiceAccounts(ctx, ada, org, false)
	require.NoError(t, err)
	assert.Empty(t, active)
	_, err = w.svc.ReactivateServiceAccount(ctx, ada, org, sa.ID())
	require.NoError(t, err)

	assert.Equal(t, []string{
		iam.EventServiceAccountCreated, iam.EventAPITokenCreated, iam.EventAPITokenRevoked,
		iam.EventServiceAccountSuspended, iam.EventServiceAccountReactivated,
	}, w.auditTypes())
	for _, e := range w.audit {
		assert.NotEmpty(t, e.DecisionID, e.Type)
		if snap, ok := e.After.(iam.APITokenSnapshot); ok {
			b, _ := json.Marshal(snap)
			assert.NotContains(t, string(b), "hash", "no audit record carries the hash")
		}
	}
}

func TestRefusals(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.CreateServiceAccount(ctx, ada, iam.CreateServiceAccountInput{OrganizationID: org})
	assert.ErrorIs(t, err, iam.ErrInvalid, "a name is required")
	_, _, err = w.svc.CreateAPIToken(ctx, ada, iam.CreateAPITokenInput{OrganizationID: org, ServiceAccountID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FZZ", Name: "x"})
	assert.ErrorIs(t, err, iam.ErrNotFound)

	w.authz.deny = func(r authorization.Request) bool { return r.Permission == iam.PermissionServiceAccountManage }
	_, err = w.svc.CreateServiceAccount(ctx, ada, iam.CreateServiceAccountInput{OrganizationID: org, DisplayName: "CI"})
	assert.ErrorIs(t, err, authorization.ErrDenied)
	_, err = w.svc.ListServiceAccounts(ctx, ada, org, false)
	require.NoError(t, err, "reading needs only service_account.read")

	inner := w.svc.(*authorized).next
	_, err = inner.CreateServiceAccount(ctx, ada, iam.CreateServiceAccountInput{OrganizationID: org, DisplayName: "CI"})
	assert.ErrorIs(t, err, change.ErrUnauthorized, "nothing changes without a decision")
}

// verifier accepts the token "ada".
type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada.PrincipalID, Kind: ada.Kind, OrganizationID: org}, nil
}

func TestOverHTTP(t *testing.T) {
	w := newWorld(t)
	port := 0
	server, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	call := func(method, path, token, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, api.BasePath+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	for _, op := range [][2]string{
		{http.MethodPost, "/service-accounts"}, {http.MethodGet, "/service-accounts"}, {http.MethodGet, "/service-accounts/x"},
		{http.MethodPost, "/service-accounts/x/suspend"}, {http.MethodPost, "/service-accounts/x/reactivate"},
		{http.MethodPost, "/service-accounts/x/tokens"}, {http.MethodGet, "/service-accounts/x/tokens"}, {http.MethodPost, "/api-tokens/x/revoke"},
	} {
		code, _ := call(op[0], op[1], "", "")
		assert.Equal(t, http.StatusUnauthorized, code, "%s %s", op[0], op[1])
	}

	code, out := call(http.MethodPost, "/service-accounts", "ada", `{"display_name":"GitHub Actions"}`)
	require.Equal(t, http.StatusCreated, code, out)
	id := out["data"].(map[string]any)["id"].(string)

	code, out = call(http.MethodPost, "/service-accounts/"+id+"/tokens", "ada", `{"name":"github-actions"}`)
	require.Equal(t, http.StatusCreated, code, out)
	issued := out["data"].(map[string]any)
	assert.True(t, iam.IsAPIToken(issued["secret"].(string)))
	token := issued["token"].(map[string]any)
	assert.NotContains(t, token, "token_hash")

	code, out = call(http.MethodGet, "/service-accounts/"+id+"/tokens", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	listed := out["data"].([]any)
	require.Len(t, listed, 1)
	assert.NotContains(t, listed[0], "secret", "a secret is shown once")

	code, out = call(http.MethodPost, "/api-tokens/"+token["id"].(string)+"/revoke", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.NotNil(t, out["data"].(map[string]any)["revoked_at"])
}
