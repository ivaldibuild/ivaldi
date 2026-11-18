package policy

// The interfaces this capability defines for itself: its repository, what
// it needs from other capabilities, and its service.

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// SettingLockRepository persists and retrieves setting locks, and nothing
// more. Every method is scoped to an Organization. Methods return
// ErrNotFound when no row matches, and ErrConflict when a lock on the key
// already exists at the level.
type SettingLockRepository interface {
	CreateSettingLock(ctx context.Context, l *SettingLock) error
	// GetSettingLock returns the lock on key at exactly the scope's level.
	GetSettingLock(ctx context.Context, scope LockScope, key string) (*SettingLock, error)
	// ListSettingLocks returns the locks set at exactly the scope's level,
	// ordered by key. There is at most one per setting definition, so the
	// list is not paged.
	ListSettingLocks(ctx context.Context, scope LockScope) ([]*SettingLock, error)
	// ListLocksInEffect returns the locks that apply at the scope: the
	// Organization's and, for a Workspace scope, that Workspace's.
	ListLocksInEffect(ctx context.Context, scope LockScope) ([]*SettingLock, error)
	// DeleteSettingLock removes a lock. History is kept by its audit record
	// and its removed event, not by the table.
	DeleteSettingLock(ctx context.Context, organizationID, lockID string) error
}

// Workspaces looks up Workspaces in the workspace capability, without
// authorizing, so a Workspace lock is only added to an active Workspace.
// workspace.NewLookups implements it.
type Workspaces interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error)
}

// SettingLockService adds, removes, and lists setting locks on behalf of an
// actor. The locks sub-package implements it, and serves it over HTTP. Its
// constructor returns the service wrapped in an authorizing layer, so the
// interface is the only way in. The workspace capability reads locks
// through NewLockSource, which does not authorize.
type SettingLockService interface {
	// ListSettingLocks returns the locks set at exactly one level.
	ListSettingLocks(ctx context.Context, actor identity.Actor, scope LockScope) ([]*SettingLock, error)
	// AddSettingLock locks a key at an Organization or an active Workspace.
	// Adding a lock that exists returns it and changes nothing.
	AddSettingLock(ctx context.Context, actor identity.Actor, in SettingLockInput) (*SettingLock, error)
	// RemoveSettingLock removes a lock, so the values below its level apply again.
	RemoveSettingLock(ctx context.Context, actor identity.Actor, in SettingLockInput) error
}

// SettingLockInput names a lock: a setting key at a level.
type SettingLockInput struct {
	Scope LockScope
	Key   string
}

// ApprovalRequestRepository persists and retrieves approval requests with
// their requirements, and nothing more. Every method is scoped to an
// Organization and returns ErrNotFound when no row matches.
type ApprovalRequestRepository interface {
	// CreateApprovalRequest inserts a request and its requirements. It
	// fails with ErrConflict when the subject already has a pending request.
	CreateApprovalRequest(ctx context.Context, r *ApprovalRequest) error
	// UpdateApprovalRequest writes a request's state and how it was closed;
	// nothing else about a request changes.
	UpdateApprovalRequest(ctx context.Context, r *ApprovalRequest) error
	GetApprovalRequest(ctx context.Context, organizationID, requestID string) (*ApprovalRequest, error)
	// GetApprovalRequestForUpdate is GetApprovalRequest that also locks the
	// row until the transaction ends.
	GetApprovalRequestForUpdate(ctx context.Context, organizationID, requestID string) (*ApprovalRequest, error)
	// GetPendingApprovalRequestForUpdate returns the subject's pending
	// request, locked until the transaction ends.
	GetPendingApprovalRequestForUpdate(ctx context.Context, organizationID string, subject Subject) (*ApprovalRequest, error)
	// ListRequiredPermissions returns the permissions the Organization's
	// requests require, sorted, so a list knows which to look up.
	ListRequiredPermissions(ctx context.Context, organizationID string) ([]string, error)
	// ListDecidableApprovalRequests lists the requests a principal could
	// decide, newest first unless the page says otherwise.
	ListDecidableApprovalRequests(ctx context.Context, q ListDecidableQuery) (*pagination.Result[*ApprovalRequest], error)
}

// ListDecidableQuery lists the requests a decider holds every requirement
// of and did not ask for, directly or through an agent Session.
type ListDecidableQuery struct {
	OrganizationID string
	DeciderID      string
	// Held maps each permission to the resources the decider holds it on;
	// it holds it beneath them too.
	Held  map[string][]string
	State ApprovalState
	// SubjectType and OwnerTeamID, when set, keep only matching requests.
	SubjectType string
	OwnerTeamID string
	Page        pagination.Query
}

// HeldResources tells where IAM grants a principal a permission.
// iam.HeldResourceReader implements it.
type HeldResources interface {
	HeldResources(ctx context.Context, q iam.HeldResourcesQuery) ([]string, error)
}

// ResourceAncestry reads a resource's path up IAM's hierarchy.
// iam.ResourceRepository implements it.
type ResourceAncestry interface {
	ResourceAncestors(ctx context.Context, organizationID, resourceID string) ([]string, error)
}

// AgentSessions reads agent Sessions: who they act for, and whether they
// were revoked. iam.AgentSessionRepository implements it.
type AgentSessions interface {
	Get(ctx context.Context, organizationID, principalID string) (*iam.AgentSession, error)
}

// Approvals opens and cancels the approval requests of capabilities whose
// subjects wait. It does not authorize: the caller already has, and its
// context carries the decision for the subject's ID, which the request's
// audit records reuse. Call it inside the transaction that makes the
// subject wait, or stop waiting, so the two change together.
type Approvals interface {
	Open(ctx context.Context, in OpenApprovalInput) (*ApprovalRequest, error)
	// Cancel cancels the subject's pending request, recording the actor
	// and why.
	Cancel(ctx context.Context, actor identity.Actor, organizationID string, subject Subject, note string) error
}

// OpenApprovalInput describes a subject that waits for approval.
type OpenApprovalInput struct {
	Subject Subject
	// Title names what waits in one line, at most MaxTitleLength characters.
	Title string
	// Request is the authorization request that waits, and Decision the
	// authorizer's requires_approval answer to it.
	Request  authorization.Request
	Decision authorization.Decision
	// Requirements are the access a decider must hold: the checks the
	// gated action made. Policy reads their resource paths from IAM.
	Requirements []Requirement
	// ReadResourceID is where approval.read is checked: the resource the
	// subject is read on.
	ReadResourceID string
	// OwnerTeamID is the Team that owns the subject, if any.
	OwnerTeamID *string
	// Summary is what approvers are shown, of the type the subject's
	// capability registered.
	Summary any
}

// ApprovalService lists, reads, and decides approval requests on behalf of
// an actor. The approvals sub-package implements it, and serves it over
// HTTP.
type ApprovalService interface {
	// ListApprovalRequests lists the requests the actor could decide, in
	// any state. A caller who is not a user gets an empty page.
	ListApprovalRequests(ctx context.Context, actor identity.Actor, q ListApprovalRequestsInput) (*pagination.Result[ApprovalRequestView], error)
	// GetApprovalRequest returns a request to anyone who can read its subject.
	GetApprovalRequest(ctx context.Context, actor identity.Actor, organizationID, requestID string) (*ApprovalRequestView, error)
	// ApproveRequest approves a pending request and applies its subject.
	ApproveRequest(ctx context.Context, actor identity.Actor, in DecideApprovalInput) (*ApprovalRequestView, error)
	// RejectRequest rejects a pending request, which needs a note.
	RejectRequest(ctx context.Context, actor identity.Actor, in DecideApprovalInput) (*ApprovalRequestView, error)
}

// ListApprovalRequestsInput filters the requests listed. State defaults to
// pending.
type ListApprovalRequestsInput struct {
	OrganizationID string
	State          ApprovalState
	SubjectType    string
	OwnerTeamID    string
	Page           pagination.Query
}

// DecideApprovalInput approves or rejects a request.
type DecideApprovalInput struct {
	OrganizationID string
	RequestID      string
	Note           string
}

// ApprovalRequestView is a request as its reader sees it.
type ApprovalRequestView struct {
	ApprovalRequestSnapshot
	// CanDecide says whether the reader could approve or reject it now.
	CanDecide bool `json:"can_decide"`
}

// DecisionProposal is the proposal an approver's authorization request
// carries: the request being decided.
type DecisionProposal struct {
	ApprovalRequestID string `json:"approval_request_id"`
}
