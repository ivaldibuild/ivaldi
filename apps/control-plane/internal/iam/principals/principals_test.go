package principals

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const (
	org          = "org_01JZ0000000000000000000001"
	ada          = "prn_01JZ00000000000000000000AD"
	releaseAgent = "prn_01JZ0000000000000000000A6E"
	ci           = "prn_01JZ00000000000000000000C1"
)

// reader returns the principals it stores, backwards, to check the service
// keeps the order the caller asked for.
type reader struct {
	principals map[string]iam.PrincipalSnapshot
	err        error
	askedOrg   string
	askedIDs   []string
}

func (r *reader) ListByIDs(_ context.Context, organizationID string, principalIDs []string) ([]iam.PrincipalSnapshot, error) {
	r.askedOrg, r.askedIDs = organizationID, principalIDs
	if r.err != nil {
		return nil, r.err
	}
	var out []iam.PrincipalSnapshot
	for i := len(principalIDs) - 1; i >= 0; i-- {
		if p, ok := r.principals[principalIDs[i]]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

type directory struct {
	teams map[string][]iam.PrincipalTeam
	asked []string
	err   error
}

func (d *directory) ListActiveTeams(_ context.Context, _ string, principalIDs []string) (map[string][]iam.PrincipalTeam, error) {
	d.asked = principalIDs
	if d.err != nil {
		return nil, d.err
	}
	out := map[string][]iam.PrincipalTeam{}
	for _, id := range principalIDs {
		out[id] = d.teams[id]
	}
	return out, nil
}

type authorizer struct {
	asked []authorization.Request
	deny  bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-1", Outcome: authorization.OutcomeAllow}
	if a.deny {
		d.Outcome, d.Reason = authorization.OutcomeDeny, "denied by the test"
	}
	return d, nil
}

func principal(id string, kind iam.PrincipalKind, name string) iam.PrincipalSnapshot {
	return iam.PrincipalSnapshot{
		ID: id, OrganizationID: org, Kind: kind, DisplayName: name, Status: iam.PrincipalActive,
	}
}

func newService(t *testing.T, r *reader, d *directory, a *authorizer) iam.PrincipalService {
	t.Helper()
	svc, err := NewPrincipalService(WithPrincipalReader(r), WithPrincipalTeamSource(d), WithAuthorizer(a))
	require.NoError(t, err)
	return svc
}

func TestNewPrincipalServiceRequiresItsDependencies(t *testing.T) {
	_, err := NewPrincipalService(WithPrincipalTeamSource(&directory{}), WithAuthorizer(&authorizer{}))
	assert.Error(t, err)
	_, err = NewPrincipalService(WithPrincipalReader(&reader{}), WithAuthorizer(&authorizer{}))
	assert.Error(t, err)
	_, err = NewPrincipalService(WithPrincipalReader(&reader{}), WithPrincipalTeamSource(&directory{}))
	assert.Error(t, err)
}

func TestListPrincipals(t *testing.T) {
	r := &reader{principals: map[string]iam.PrincipalSnapshot{
		ada:          principal(ada, iam.PrincipalUser, "Ada Lovelace"),
		releaseAgent: principal(releaseAgent, iam.PrincipalAgent, "Release assistant"),
		ci:           principal(ci, iam.PrincipalServiceAccount, "payments-ci"),
	}}
	d := &directory{teams: map[string][]iam.PrincipalTeam{
		releaseAgent: {{ID: "tem_platform", Slug: "platform", DisplayName: "Platform"}},
	}}
	authz := &authorizer{}
	svc := newService(t, r, d, authz)
	actor := identity.Actor{PrincipalID: ada, Kind: identity.ActorUser}

	found, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{
		OrganizationID: org, IDs: []string{releaseAgent, "", ada, releaseAgent, "prn_missing"},
	})
	require.NoError(t, err)
	require.Len(t, found, 2, "unknown IDs are left out and repeats are asked once")
	assert.Equal(t, releaseAgent, found[0].Principal.ID, "the caller's order is kept")
	assert.Equal(t, ada, found[1].Principal.ID)
	assert.Equal(t, []iam.PrincipalTeam{{ID: "tem_platform", Slug: "platform", DisplayName: "Platform"}}, found[0].Teams,
		"an Agent names its active Teams")
	assert.Empty(t, found[1].Teams, "a user has no Teams here")
	assert.Equal(t, []string{releaseAgent, ada, "prn_missing"}, r.askedIDs, "every distinct ID is asked for; the reader leaves out the unknown")
	assert.Equal(t, org, r.askedOrg)
	assert.Equal(t, []string{releaseAgent}, d.asked, "only Agents' Teams are read")

	require.Len(t, authz.asked, 1)
	assert.Equal(t, iam.PermissionPrincipalRead, authz.asked[0].Permission)
	assert.Equal(t, iam.ActionPrincipalList, authz.asked[0].Action)
	assert.Equal(t, org, authz.asked[0].ResourceID)

	t.Run("no IDs reads nothing", func(t *testing.T) {
		r.askedIDs = nil
		found, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{OrganizationID: org})
		require.NoError(t, err)
		assert.Empty(t, found)
		assert.Nil(t, r.askedIDs)
	})

	t.Run("refuses more than 100 IDs", func(t *testing.T) {
		ids := make([]string, 101)
		for i := range ids {
			ids[i] = "prn_" + strings.Repeat("a", i+1)
		}
		_, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{OrganizationID: org, IDs: ids})
		assert.ErrorIs(t, err, iam.ErrInvalid)
	})

	t.Run("denied", func(t *testing.T) {
		authz.deny = true
		_, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{OrganizationID: org, IDs: []string{ada}})
		assert.ErrorIs(t, err, authorization.ErrDenied)
		authz.deny = false
	})

	t.Run("a read failure is returned", func(t *testing.T) {
		boom := errors.New("database unavailable")
		r.err = boom
		_, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{OrganizationID: org, IDs: []string{ada}})
		assert.ErrorIs(t, err, boom)
		r.err = nil
	})

	t.Run("a Team read failure is returned", func(t *testing.T) {
		boom := errors.New("teams unavailable")
		d.err = boom
		_, err := svc.ListPrincipals(context.Background(), actor, iam.ListPrincipalsQuery{OrganizationID: org, IDs: []string{releaseAgent}})
		assert.ErrorIs(t, err, boom)
		d.err = nil
	})
}

func TestListPrincipalsOverHTTP(t *testing.T) {
	r := &reader{principals: map[string]iam.PrincipalSnapshot{
		ada:          principal(ada, iam.PrincipalUser, "Ada Lovelace"),
		releaseAgent: principal(releaseAgent, iam.PrincipalAgent, "Release assistant"),
	}}
	d := &directory{teams: map[string][]iam.PrincipalTeam{
		releaseAgent: {{ID: "tem_platform", Slug: "platform", DisplayName: "Platform"}},
	}}

	t.Run("needs a token", func(t *testing.T) {
		rec, _ := get(t, r, d, &authorizer{}, "/principals?ids="+ada, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("names the token's Organization's principals", func(t *testing.T) {
		rec, out := get(t, r, d, &authorizer{}, "/principals?ids="+url.QueryEscape(releaseAgent+","+ada+",prn_missing"), "ada")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		items := out["data"].([]any)
		require.Len(t, items, 2)
		agent := items[0].(map[string]any)
		assert.ElementsMatch(t, []string{"id", "kind", "display_name", "status", "teams"}, keys(agent))
		assert.Equal(t, releaseAgent, agent["id"])
		assert.Equal(t, "agent", agent["kind"])
		assert.Equal(t, "Release assistant", agent["display_name"])
		assert.Equal(t, "active", agent["status"])
		assert.Equal(t, []any{map[string]any{"id": "tem_platform", "slug": "platform", "display_name": "Platform"}}, agent["teams"])

		user := items[1].(map[string]any)
		assert.Equal(t, ada, user["id"])
		assert.Equal(t, []any{}, user["teams"], "a list is [], not null")
	})

	t.Run("refusals", func(t *testing.T) {
		rec, _ := get(t, r, d, &authorizer{deny: true}, "/principals?ids="+ada, "ada")
		assert.Equal(t, http.StatusForbidden, rec.Code)

		rec, _ = get(t, r, d, &authorizer{}, "/principals", "ada")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "ids is required")

		tooMany := make([]string, 101)
		for i := range tooMany {
			tooMany[i] = "prn_" + strings.Repeat("a", i+1)
		}
		rec, _ = get(t, r, d, &authorizer{}, "/principals?ids="+strings.Join(tooMany, ","), "ada")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "at most 100 distinct IDs")

		rec, _ = get(t, r, d, &authorizer{}, "/principals?ids="+strings.Repeat(ada+",", 150)+ada, "ada")
		assert.Equal(t, http.StatusOK, rec.Code, "repeated IDs do not count against the limit")

		rec, _ = get(t, &reader{err: iam.NewErrInvalidInput("bad ids")}, d, &authorizer{}, "/principals?ids="+ada, "ada")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

		rec, _ = get(t, &reader{err: errors.New(`pq: relation "iam.principals" does not exist`)}, d, &authorizer{}, "/principals?ids="+ada, "ada")
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotContains(t, rec.Body.String(), "iam.principals")
	})
}

type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada, Kind: identity.ActorUser, OrganizationID: org}, nil
}

func get(t *testing.T, r *reader, d *directory, authz *authorizer, path, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	svc := newService(t, r, d, authz)
	port := 0
	logger := logging.NewMockLogger()
	logger.On("Error", "request failed", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	s, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logger,
		api.WithRoutes(NewController(svc)), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, api.BasePath+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
