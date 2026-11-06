package people

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org = "org_01JZ0000000000000000000001"
	ada = "prn_01JZ00000000000000000000AD"
)

// repo returns its people and remembers the queries it was asked.
type repo struct {
	people []iam.Person
	err    error
	asked  []iam.ListPeopleQuery
}

func (r *repo) ListPeople(_ context.Context, q iam.ListPeopleQuery) (*pagination.Result[iam.Person], error) {
	r.asked = append(r.asked, q)
	if r.err != nil {
		return nil, r.err
	}
	return &pagination.Result[iam.Person]{Items: r.people, TotalCount: len(r.people), HasNext: true, NextCursor: new("next")}, nil
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

var signedIn = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func grace() iam.Person {
	return iam.Person{
		User: iam.UserSnapshot{
			PrincipalSnapshot: iam.PrincipalSnapshot{ID: "prn_grace", OrganizationID: org, Kind: iam.PrincipalUser, DisplayName: "Grace", Status: iam.PrincipalActive},
			AccountID:         "acc_grace", LastSignedInAt: &signedIn,
		},
		Email: "grace@example.com",
		Roles: []iam.HeldRole{{Key: iam.RoleAdmin, RoleID: "rol_00000000000000000000000002", ResourceID: org}},
	}
}

func TestNewPeopleServiceRequiresItsDependencies(t *testing.T) {
	_, err := NewPeopleService(WithAuthorizer(&authorizer{}))
	assert.Error(t, err)
	_, err = NewPeopleService(WithPeopleRepo(&repo{}))
	assert.Error(t, err)
}

func TestListPeople(t *testing.T) {
	r, authz := &repo{people: []iam.Person{grace()}}, &authorizer{}
	svc, err := NewPeopleService(WithPeopleRepo(r), WithAuthorizer(authz))
	require.NoError(t, err)
	actor := identity.Actor{PrincipalID: ada, Kind: identity.ActorUser}

	page, err := svc.ListPeople(context.Background(), actor, iam.ListPeopleQuery{OrganizationID: org, Search: "gra"})
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	require.Len(t, authz.asked, 1)
	assert.Equal(t, iam.PermissionUserRead, authz.asked[0].Permission)
	assert.Equal(t, iam.ActionPeopleList, authz.asked[0].Action)
	assert.Equal(t, org, authz.asked[0].ResourceID)

	_, err = svc.ListPeople(context.Background(), actor, iam.ListPeopleQuery{OrganizationID: org, Search: strings.Repeat("a", 321)})
	assert.ErrorIs(t, err, iam.ErrInvalid)

	authz.deny = true
	_, err = svc.ListPeople(context.Background(), actor, iam.ListPeopleQuery{OrganizationID: org})
	assert.ErrorIs(t, err, authorization.ErrDenied)
}

type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada, Kind: identity.ActorUser, OrganizationID: org}, nil
}

func get(t *testing.T, r *repo, authz *authorizer, path, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	svc, err := NewPeopleService(WithPeopleRepo(r), WithAuthorizer(authz))
	require.NoError(t, err)
	port := 0
	logger := logging.NewMockLogger()
	logger.On("Error", "request failed", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	s, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logger,
		api.WithRoutes(NewPeopleController(svc)), api.WithVerifier(verifier{}))
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

func TestListPeopleOverHTTP(t *testing.T) {
	t.Run("needs a token", func(t *testing.T) {
		rec, _ := get(t, &repo{}, &authorizer{}, "/people", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("lists the token's Organization's people", func(t *testing.T) {
		r := &repo{people: []iam.Person{grace()}}
		rec, out := get(t, r, &authorizer{}, "/people?q=gra&include_inactive=true&limit=10&sort_by=created_at&sort_order=desc&next_cursor=abc", "ada")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		require.Len(t, r.asked, 1)
		q := r.asked[0]
		assert.Equal(t, org, q.OrganizationID)
		assert.Equal(t, "gra", q.Search)
		assert.True(t, q.IncludeInactive)
		assert.Equal(t, 10, q.Page.Limit)
		assert.Equal(t, "created_at", q.Page.SortBy)
		assert.Equal(t, "desc", q.Page.SortOrder)
		assert.Equal(t, new("abc"), q.Page.NextCursor)

		data := out["data"].(map[string]any)
		assert.Equal(t, "next", data["next_cursor"])
		p := data["items"].([]any)[0].(map[string]any)
		assert.ElementsMatch(t, []string{"id", "display_name", "email", "status", "created_at", "joined_at", "last_signed_in_at", "roles"}, keys(p))
		assert.Equal(t, "grace@example.com", p["email"])
		assert.Equal(t, "2026-11-06T12:00:00Z", p["last_signed_in_at"])
		assert.Equal(t, []any{map[string]any{"role": "admin", "role_id": "rol_00000000000000000000000002"}}, p["roles"])
	})

	t.Run("refusals", func(t *testing.T) {
		rec, _ := get(t, &repo{}, &authorizer{deny: true}, "/people", "ada")
		assert.Equal(t, http.StatusForbidden, rec.Code)
		rec, _ = get(t, &repo{}, &authorizer{}, "/people?sort_by=email", "ada")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		rec, _ = get(t, &repo{err: iam.NewErrInvalidInput("bad cursor")}, &authorizer{}, "/people", "ada")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		rec, _ = get(t, &repo{err: errors.New(`pq: relation "iam.users" does not exist`)}, &authorizer{}, "/people", "ada")
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotContains(t, rec.Body.String(), "iam.users")
	})
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
