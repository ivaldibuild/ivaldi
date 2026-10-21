package iam

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const (
	sessionProject = "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sessionSource  = "src_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

func sessionProps() NewAgentSessionProps {
	props := principalProps()
	props.DisplayName = "Session for WI-42"
	props.Metadata = nil
	return NewAgentSessionProps{
		PrincipalProps:       props,
		SessionID:            "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		WorkItemID:           "wki_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		DelegatorPrincipalID: testPrincipal,
		ExpiresAt:            t2,
		ScopeResourceIDs:     []string{sessionProject, testWorkspace},
	}
}

func sessionNew(t *testing.T) *AgentSession {
	t.Helper()
	s, err := NewAgentSession(sessionProps(), nil)
	require.NoError(t, err)
	return s
}

func TestNewAgentSession(t *testing.T) {
	clock.Fix(t, t0)

	s, err := NewAgentSession(sessionProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("prn", s.ID()), "an agent session gets a prn_ ID: %q", s.ID())
	assert.Equal(t, PrincipalAgentSession, s.Kind())
	assert.Equal(t, "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV", s.SessionID())
	assert.Equal(t, "wki_01ARZ3NDEKTSV4RRFFQ69G5FAV", s.WorkItemID())
	assert.Equal(t, testPrincipal, s.DelegatorPrincipalID())
	assert.Equal(t, t2, s.ExpiresAt())
	assert.Nil(t, s.RevokedAt())
	assert.False(t, s.Revoked())
	assert.False(t, s.Expired())
	assert.Equal(t, []string{sessionProject, testWorkspace}, s.ScopeResourceIDs(), "scope resources are sorted")
	assert.NoError(t, s.CheckUsable())
}

func TestNewAgentSessionWithStoredState(t *testing.T) {
	clock.Fix(t, t2.Add(time.Hour))
	given := id.New("prn")
	props := sessionProps()
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)
	props.RevokedAt = new(t1)

	s, err := NewAgentSession(props, &given)

	require.NoError(t, err, "a stored session that has since expired still loads")
	assert.Equal(t, given, s.ID())
	assert.Equal(t, t0, s.CreatedAt())
	assert.Equal(t, t1, s.UpdatedAt())
	require.NotNil(t, s.RevokedAt())
	assert.Equal(t, t1, *s.RevokedAt())
	assert.True(t, s.Revoked())
	assert.True(t, s.Expired())

	*props.RevokedAt = t2
	assert.Equal(t, t1, *s.RevokedAt(), "the given time is copied")
}

func TestNewAgentSessionIsRefused(t *testing.T) {
	self := id.New("prn")
	tests := []struct {
		name   string
		mutate func(*NewAgentSessionProps)
		id     *string
	}{
		{"no organization", func(p *NewAgentSessionProps) { p.OrganizationID = "" }, nil},
		{"no session ID", func(p *NewAgentSessionProps) { p.SessionID = "" }, nil},
		{"session ID too long", func(p *NewAgentSessionProps) { p.SessionID = strings.Repeat("s", maxReferenceLength+1) }, nil},
		{"no work item ID", func(p *NewAgentSessionProps) { p.WorkItemID = "" }, nil},
		{"work item ID too long", func(p *NewAgentSessionProps) { p.WorkItemID = strings.Repeat("w", maxReferenceLength+1) }, nil},
		{"no delegator", func(p *NewAgentSessionProps) { p.DelegatorPrincipalID = "" }, nil},
		{"delegator is an account", func(p *NewAgentSessionProps) { p.DelegatorPrincipalID = testAccount }, nil},
		{"delegates to itself", func(p *NewAgentSessionProps) { p.DelegatorPrincipalID = self }, &self},
		{"no expiry", func(p *NewAgentSessionProps) { p.ExpiresAt = time.Time{} }, nil},
		{"expires when created", func(p *NewAgentSessionProps) { p.ExpiresAt = t0 }, nil},
		{"expires before created", func(p *NewAgentSessionProps) { p.ExpiresAt = t0.Add(-time.Minute) }, nil},
		{"stored expiry before creation", func(p *NewAgentSessionProps) { p.CreatedAt = new(t2); p.ExpiresAt = t1 }, nil},
		{"no scope", func(p *NewAgentSessionProps) { p.ScopeResourceIDs = nil }, nil},
		{"empty scope resource", func(p *NewAgentSessionProps) { p.ScopeResourceIDs = []string{""} }, nil},
		{"scope resource too long", func(p *NewAgentSessionProps) {
			p.ScopeResourceIDs = []string{strings.Repeat("r", maxReferenceLength+1)}
		}, nil},
		{"scope resource twice", func(p *NewAgentSessionProps) { p.ScopeResourceIDs = []string{sessionProject, sessionProject} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := sessionProps()
			tt.mutate(&props)
			_, err := NewAgentSession(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAgentSessionGettersReturnCopies(t *testing.T) {
	clock.Fix(t, t0)
	props := sessionProps()
	props.RevokedAt = new(t1)
	s, err := NewAgentSession(props, nil)
	require.NoError(t, err)

	props.ScopeResourceIDs[0] = "changed"
	s.ScopeResourceIDs()[0] = "changed"
	*s.RevokedAt() = t2
	snap := s.Snapshot()
	snap.ScopeResourceIDs[0] = "changed"
	*snap.RevokedAt = t2

	assert.Equal(t, []string{sessionProject, testWorkspace}, s.ScopeResourceIDs())
	assert.Equal(t, t1, *s.RevokedAt())
}

func TestAgentSessionSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	s := sessionNew(t)

	assert.Equal(t, AgentSessionSnapshot{
		PrincipalSnapshot: PrincipalSnapshot{
			ID:             s.ID(),
			OrganizationID: testOrg,
			Kind:           PrincipalAgentSession,
			DisplayName:    "Session for WI-42",
			Status:         PrincipalActive,
			CreatedAt:      t0,
			UpdatedAt:      t0,
		},
		SessionID:            "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		WorkItemID:           "wki_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		DelegatorPrincipalID: testPrincipal,
		ExpiresAt:            t2,
		ScopeResourceIDs:     []string{sessionProject, testWorkspace},
	}, s.Snapshot())
}

func TestAgentSessionCheckUsable(t *testing.T) {
	t.Run("usable until it expires", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)

		clock.Fix(t, t2.Add(-time.Microsecond))
		assert.NoError(t, s.CheckUsable())

		clock.Fix(t, t2)
		assert.True(t, s.Expired(), "a session expires at its expiry time")
		assert.ErrorIs(t, s.CheckUsable(), ErrExpired)
	})

	t.Run("revoked", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		s.Revoke()

		assert.ErrorIs(t, s.CheckUsable(), ErrRevoked)
	})

	t.Run("revocation wins over expiry", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		s.Revoke()
		clock.Fix(t, t2)

		assert.ErrorIs(t, s.CheckUsable(), ErrRevoked)
	})

	t.Run("suspended", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		require.NoError(t, s.Suspend())

		assert.ErrorIs(t, s.CheckUsable(), ErrStatus)
	})

	t.Run("deactivated", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		require.NoError(t, s.Deactivate())

		assert.ErrorIs(t, s.CheckUsable(), ErrStatus)
	})
}

func TestAgentSessionCovers(t *testing.T) {
	clock.Fix(t, t0)
	s := sessionNew(t)

	assert.True(t, s.Covers([]string{sessionProject, testWorkspace, testOrg}), "a scope resource itself")
	assert.True(t, s.Covers([]string{sessionSource, sessionProject, testWorkspace, testOrg}), "a descendant of a scope resource")
	assert.True(t, s.Covers([]string{testWorkspace, testOrg}))
	assert.False(t, s.Covers([]string{testOrg}), "an ancestor of every scope resource")
	assert.False(t, s.Covers([]string{"prj_01BX5ZZKBKACTAV9WEVGEMMVRZ", "wsp_01BX5ZZKBKACTAV9WEVGEMMVRZ", testOrg}), "another branch")
	assert.False(t, s.Covers(nil))
}

func TestAgentSessionRevoke(t *testing.T) {
	clock.Fix(t, t0)
	s := sessionNew(t)
	clock.Fix(t, t1)

	s.Revoke()

	require.True(t, s.Revoked())
	assert.Equal(t, t1, *s.RevokedAt())
	assert.Equal(t, t1, s.UpdatedAt())

	clock.Fix(t, t2)
	s.Revoke()
	assert.Equal(t, t1, *s.RevokedAt(), "revoking a revoked session does nothing")
	assert.Equal(t, t1, s.UpdatedAt())
}

func TestAgentSessionReactivate(t *testing.T) {
	t.Run("suspended session", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		require.NoError(t, s.Suspend())

		require.NoError(t, s.Reactivate())
		assert.Equal(t, PrincipalActive, s.Status())
		assert.NoError(t, s.CheckUsable())
	})

	t.Run("revoked session", func(t *testing.T) {
		clock.Fix(t, t0)
		s := sessionNew(t)
		require.NoError(t, s.Suspend())
		s.Revoke()

		assert.ErrorIs(t, s.Reactivate(), ErrRevoked)
		assert.Equal(t, PrincipalSuspended, s.Status(), "nothing changes")
	})
}
