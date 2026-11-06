package apitokens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	org = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ci  = "prn_01ARZ3NDEKTSV4RRFFQ69G5FC0"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// store holds tokens by hash and principals by ID, and counts writes.
type store struct {
	tokens     map[string]*iam.APIToken
	principals map[string]iam.PrincipalSnapshot
	writes     int
}

func (s *store) GetByHash(_ context.Context, hash []byte) (*iam.APIToken, error) {
	t, ok := s.tokens[string(hash)]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return t, nil
}

func (s *store) Write(_ context.Context, t *iam.APIToken) error {
	s.writes++
	return nil
}

func (s *store) Get(_ context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error) {
	p, ok := s.principals[principalID]
	if !ok || p.OrganizationID != organizationID {
		return iam.PrincipalSnapshot{}, iam.ErrNotFound
	}
	return p, nil
}

// accessTokens accepts "jwt" and refuses everything else.
type accessTokens struct{}

func (accessTokens) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token == "jwt" {
		return &identity.Claims{Subject: "prn_person", Kind: identity.ActorUser}, nil
	}
	return nil, identity.ErrInvalidToken
}

func setup(t *testing.T, expires *time.Time) (*store, identity.Verifier, string, *iam.APIToken) {
	t.Helper()
	clock.Fix(t, t0)
	issued, err := iam.NewAPITokenSecret()
	require.NoError(t, err)
	tok, err := iam.NewAPIToken(iam.NewAPITokenProps{
		OrganizationID: org, PrincipalID: ci, Name: "github-actions",
		TokenPrefix: issued.Prefix, TokenHash: issued.Hash, ExpiresAt: expires,
	}, nil)
	require.NoError(t, err)
	s := &store{
		tokens:     map[string]*iam.APIToken{string(issued.Hash): tok},
		principals: map[string]iam.PrincipalSnapshot{ci: {ID: ci, OrganizationID: org, Kind: iam.PrincipalServiceAccount, Status: iam.PrincipalActive}},
	}
	v, err := NewVerifier(s, s, accessTokens{})
	require.NoError(t, err)
	return s, v, issued.Secret, tok
}

func TestAnAPITokenActsAsItsServiceAccount(t *testing.T) {
	s, v, secret, tok := setup(t, nil)
	assert.True(t, iam.IsAPIToken(secret))

	claims, err := v.Verify(context.Background(), secret)
	require.NoError(t, err)
	assert.Equal(t, ci, claims.Subject)
	assert.Equal(t, org, claims.OrganizationID)
	assert.Equal(t, identity.ActorServiceAccount, claims.Kind)
	assert.Equal(t, ClientID, claims.ClientID)
	assert.Equal(t, tok.ID(), claims.ID)

	t.Run("records its use, at most once a minute", func(t *testing.T) {
		assert.Equal(t, 1, s.writes)
		assert.Equal(t, &t0, tok.LastUsedAt())
		_, err := v.Verify(context.Background(), secret)
		require.NoError(t, err)
		assert.Equal(t, 1, s.writes)
		clock.Fix(t, t0.Add(time.Minute))
		_, err = v.Verify(context.Background(), secret)
		require.NoError(t, err)
		assert.Equal(t, 2, s.writes)
	})
}

func TestOtherTokensGoToTheAccessTokenVerifier(t *testing.T) {
	_, v, _, _ := setup(t, nil)
	claims, err := v.Verify(context.Background(), "jwt")
	require.NoError(t, err)
	assert.Equal(t, "prn_person", claims.Subject)
	_, err = v.Verify(context.Background(), "garbage")
	assert.ErrorIs(t, err, identity.ErrInvalidToken)
}

func TestRefusedAPITokens(t *testing.T) {
	cases := map[string]struct {
		edit func(s *store, tok *iam.APIToken)
		want error
	}{
		"revoked": {edit: func(_ *store, tok *iam.APIToken) { tok.Revoke() }, want: identity.ErrInvalidToken},
		"expired": {edit: func(*store, *iam.APIToken) { clock.Fix(t, t0.Add(2*time.Hour)) }, want: identity.ErrTokenExpired},
		"a suspended owner": {edit: func(s *store, _ *iam.APIToken) {
			p := s.principals[ci]
			p.Status = iam.PrincipalSuspended
			s.principals[ci] = p
		}, want: identity.ErrInvalidToken},
		"an owner that is gone": {edit: func(s *store, _ *iam.APIToken) { delete(s.principals, ci) }, want: identity.ErrInvalidToken},
		"an agent session's": {edit: func(s *store, _ *iam.APIToken) {
			p := s.principals[ci]
			p.Kind = iam.PrincipalAgentSession
			s.principals[ci] = p
		}, want: identity.ErrInvalidToken},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, v, secret, tok := setup(t, new(t0.Add(time.Hour)))
			tc.edit(s, tok)
			_, err := v.Verify(context.Background(), secret)
			assert.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("an unknown token", func(t *testing.T) {
		_, v, _, _ := setup(t, nil)
		_, err := v.Verify(context.Background(), iam.APITokenPrefix+"nope")
		assert.ErrorIs(t, err, identity.ErrInvalidToken)
	})

	t.Run("a lookup failure is not an invalid token", func(t *testing.T) {
		_, err := (&verifier{tokens: failing{}, principals: &store{}, next: accessTokens{}}).Verify(context.Background(), iam.APITokenPrefix+"x")
		require.Error(t, err)
		assert.False(t, errors.Is(err, identity.ErrInvalidToken), "the API answers 500, not 401")
	})
}

type failing struct{}

func (failing) GetByHash(context.Context, []byte) (*iam.APIToken, error) {
	return nil, errors.New("database down")
}
func (failing) Write(context.Context, *iam.APIToken) error { return nil }

func TestNewVerifierRequiresItsDependencies(t *testing.T) {
	s := &store{}
	for _, args := range [][3]any{{nil, s, accessTokens{}}, {s, nil, accessTokens{}}, {s, s, nil}} {
		var tokens Tokens
		var principals Principals
		var next identity.Verifier
		if args[0] != nil {
			tokens = args[0].(Tokens)
		}
		if args[1] != nil {
			principals = args[1].(Principals)
		}
		if args[2] != nil {
			next = args[2].(identity.Verifier)
		}
		_, err := NewVerifier(tokens, principals, next)
		assert.Error(t, err)
	}
}
