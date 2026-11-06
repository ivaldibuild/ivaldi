package authorization

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authorizerFunc func(Request) (Decision, error)

func (f authorizerFunc) Authorize(_ context.Context, req Request) (Decision, error) { return f(req) }

func TestRequire(t *testing.T) {
	req := Request{Permission: "workspace.manage", Action: "workspace.create"}

	tests := []struct {
		name     string
		decision Decision
		err      error
		wantErr  error
	}{
		{name: "allowed", decision: Decision{ID: "dec_1", Outcome: OutcomeAllow}},
		{name: "denied", decision: Decision{ID: "dec_1", Outcome: OutcomeDeny, Reason: "no grant"}, wantErr: ErrDenied},
		{name: "needs approval", decision: Decision{ID: "dec_1", Outcome: OutcomeRequiresApproval}, wantErr: ErrApprovalRequired},
		{name: "unknown outcome is a denial", decision: Decision{ID: "dec_1", Outcome: "maybe"}, wantErr: ErrDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Require(context.Background(), authorizerFunc(func(Request) (Decision, error) { return tt.decision, nil }), req)

			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, tt.decision, got, "the decision is returned either way, so callers can record it")
		})
	}

	t.Run("the approval error carries the decision to resubmit with", func(t *testing.T) {
		_, err := Require(context.Background(), authorizerFunc(func(Request) (Decision, error) {
			return Decision{ID: "dec_pending", Outcome: OutcomeRequiresApproval}, nil
		}), req)
		var approval *ApprovalRequiredError
		require.ErrorAs(t, err, &approval)
		assert.Equal(t, "dec_pending", approval.DecisionID)
	})

	t.Run("a denial reports its reason", func(t *testing.T) {
		_, err := Require(context.Background(), authorizerFunc(func(Request) (Decision, error) {
			return Decision{ID: "dec_1", Outcome: OutcomeDeny, Reason: "locked by the organization"}, nil
		}), req)
		assert.EqualError(t, err, "denied: locked by the organization (decision dec_1)")
	})

	t.Run("an authorizer failure is not a denial", func(t *testing.T) {
		boom := errors.New("policy unavailable")
		_, err := Require(context.Background(), authorizerFunc(func(Request) (Decision, error) { return Decision{}, boom }), req)
		require.ErrorIs(t, err, boom)
		assert.NotErrorIs(t, err, ErrDenied)
		assert.Contains(t, err.Error(), "authorize workspace.create")
	})
}

func TestDecisionInContext(t *testing.T) {
	ctx := context.Background()
	_, ok := DecisionFor(ctx, "env_1")
	assert.False(t, ok, "no decision, no authorization")

	call := Decision{ID: "dec_call", Outcome: OutcomeAllow}
	ctx = WithDecision(ctx, "", call)
	got, ok := DecisionFor(ctx, "env_1")
	require.True(t, ok)
	assert.Equal(t, call, got, "the call's decision covers every subject")

	one := Decision{ID: "dec_env_1", Outcome: OutcomeAllow}
	withOne := WithDecision(ctx, "env_1", one)
	got, _ = DecisionFor(withOne, "env_1")
	assert.Equal(t, one, got, "a subject's own decision wins")
	got, _ = DecisionFor(withOne, "env_2")
	assert.Equal(t, call, got)

	got, _ = DecisionFor(ctx, "env_1")
	assert.Equal(t, call, got, "adding a decision leaves the parent context as it was")
}
