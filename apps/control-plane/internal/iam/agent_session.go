package iam

import (
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// AgentSession is the identity of one orchestrated Session. It acts for its
// delegator: its access is the delegator's, narrowed to its scope, and only
// until it expires or is revoked. It owns its scope, the resources it is
// limited to, which is fixed when it is created.
type AgentSession struct {
	principal
	sessionID            string
	workItemID           string
	delegatorPrincipalID string
	expiresAt            time.Time
	revokedAt            *time.Time
	scopeResourceIDs     []string
}

// AgentSessionSnapshot is an AgentSession's state, for repositories and
// audit records.
type AgentSessionSnapshot struct {
	PrincipalSnapshot
	SessionID            string
	WorkItemID           string
	DelegatorPrincipalID string
	ExpiresAt            time.Time
	RevokedAt            *time.Time
	// ScopeResourceIDs are sorted: their order means nothing.
	ScopeResourceIDs []string
}

// NewAgentSessionProps describes an AgentSession. A new one leaves
// RevokedAt nil; a repository loading a stored one sets it.
type NewAgentSessionProps struct {
	PrincipalProps
	// SessionID and WorkItemID are owned by the orchestration capability.
	SessionID  string
	WorkItemID string
	// DelegatorPrincipalID is the user or service account the Session acts for.
	DelegatorPrincipalID string
	ExpiresAt            time.Time
	RevokedAt            *time.Time
	// ScopeResourceIDs are the resources the Session is limited to, together
	// with everything beneath them.
	ScopeResourceIDs []string
}

// NewAgentSession returns an AgentSession, with a new ID unless principalID
// gives a stored one.
func NewAgentSession(props NewAgentSessionProps, principalID *string) (*AgentSession, error) {
	p, err := newPrincipal(PrincipalAgentSession, props.PrincipalProps, principalID)
	if err != nil {
		return nil, err
	}
	if err := validateReference("session ID", props.SessionID); err != nil {
		return nil, err
	}
	if err := validateReference("work item ID", props.WorkItemID); err != nil {
		return nil, err
	}
	if err := validateID("delegator", principalPrefix, props.DelegatorPrincipalID); err != nil {
		return nil, err
	}
	if props.DelegatorPrincipalID == p.ID() {
		return nil, NewErrInvalidInput("an agent session cannot delegate to itself")
	}
	if err := validateExpiry(props.ExpiresAt, p.createdAt); err != nil {
		return nil, err
	}
	if err := validateScope(props.ScopeResourceIDs); err != nil {
		return nil, err
	}
	return &AgentSession{
		principal:            p,
		sessionID:            props.SessionID,
		workItemID:           props.WorkItemID,
		delegatorPrincipalID: props.DelegatorPrincipalID,
		expiresAt:            props.ExpiresAt,
		revokedAt:            clock.CopyTime(props.RevokedAt),
		scopeResourceIDs:     sortedStrings(props.ScopeResourceIDs),
	}, nil
}

// validateReference checks the ID of a record another capability owns,
// which fits the varchar(30) column.
func validateReference(field, value string) error {
	if value == "" || len(value) > maxReferenceLength {
		return NewErrInvalidInput(fmt.Sprintf("%s must be 1-%d characters", field, maxReferenceLength))
	}
	return nil
}

func validateScope(resourceIDs []string) error {
	if len(resourceIDs) == 0 {
		return NewErrInvalidInput("an agent session needs at least one scope resource")
	}
	for i, r := range resourceIDs {
		if err := validateReference("scope resource ID", r); err != nil {
			return err
		}
		if slices.Contains(resourceIDs[:i], r) {
			return NewErrInvalidInput(fmt.Sprintf("scope resource %s appears twice", r))
		}
	}
	return nil
}

func (s *AgentSession) SessionID() string            { return s.sessionID }
func (s *AgentSession) WorkItemID() string           { return s.workItemID }
func (s *AgentSession) DelegatorPrincipalID() string { return s.delegatorPrincipalID }
func (s *AgentSession) ExpiresAt() time.Time         { return s.expiresAt }
func (s *AgentSession) RevokedAt() *time.Time        { return clock.CopyTime(s.revokedAt) }
func (s *AgentSession) ScopeResourceIDs() []string   { return copyStrings(s.scopeResourceIDs) }

func (s *AgentSession) Revoked() bool { return s.revokedAt != nil }
func (s *AgentSession) Expired() bool { return !clock.Now().Before(s.expiresAt) }

// CheckUsable returns an error unless the Session may act now: it must be
// active, not revoked, and not expired. Its delegator must be checked too.
func (s *AgentSession) CheckUsable() error {
	switch {
	case s.Revoked():
		return fmt.Errorf("%w: agent session %s", ErrRevoked, s.ID())
	case s.Expired():
		return fmt.Errorf("%w: agent session %s", ErrExpired, s.ID())
	case !s.Active():
		return fmt.Errorf("%w: agent session %s is %s", ErrStatus, s.ID(), s.status)
	}
	return nil
}

// Covers reports whether a resource is within the Session's scope, given
// the resource's ID and its ancestors' IDs: it is if any of them is a scope
// resource.
func (s *AgentSession) Covers(resourceAndAncestorIDs []string) bool {
	for _, r := range resourceAndAncestorIDs {
		if _, found := slices.BinarySearch(s.scopeResourceIDs, r); found {
			return true
		}
	}
	return false
}

func (s *AgentSession) Snapshot() AgentSessionSnapshot {
	return AgentSessionSnapshot{
		PrincipalSnapshot:    s.snapshot(),
		SessionID:            s.sessionID,
		WorkItemID:           s.workItemID,
		DelegatorPrincipalID: s.delegatorPrincipalID,
		ExpiresAt:            s.expiresAt,
		RevokedAt:            clock.CopyTime(s.revokedAt),
		ScopeResourceIDs:     copyStrings(s.scopeResourceIDs),
	}
}

// Revoke ends the Session's access for good. Revoking a revoked Session
// does nothing.
func (s *AgentSession) Revoke() {
	if !s.Revoked() {
		now := clock.Now()
		s.revokedAt = &now
		s.updatedAt = now
	}
}

// Reactivate makes a suspended Session active again. A revoked Session
// cannot be reactivated.
func (s *AgentSession) Reactivate() error {
	if s.Revoked() {
		return fmt.Errorf("%w: agent session %s", ErrRevoked, s.ID())
	}
	return s.principal.Reactivate()
}
