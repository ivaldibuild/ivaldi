// Package approvals implements the policy capability's approval requests:
// policy.Approvals, through which the capabilities whose subjects wait open
// and cancel requests, and policy.ApprovalService, which lists, reads, and
// decides them, with its HTTP API. The domain type stays in policy; this
// package orchestrates it. The design is in docs/design/approvals.md.
//
// NewService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. NewApprovals does not
// authorize: its callers have, inside the transaction that changes the
// subject.
package approvals

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type Option func(*options)

type options struct {
	requests policy.ApprovalRequestRepository
	subjects *policy.Subjects
	held     policy.HeldResources
	ancestry policy.ResourceAncestry
	sessions policy.AgentSessions
	authz    authorization.Authorizer
	writer   change.Writer
}

func WithApprovalRequestRepo(r policy.ApprovalRequestRepository) Option {
	return func(o *options) { o.requests = r }
}

// WithSubjects is the registry of subject types, whose handlers apply or
// refuse a subject when its request is decided.
func WithSubjects(s *policy.Subjects) Option { return func(o *options) { o.subjects = s } }

// WithHeldResources tells where IAM grants a decider each requirement's
// permission (iam.HeldResourceReader).
func WithHeldResources(h policy.HeldResources) Option { return func(o *options) { o.held = h } }

// WithResourceAncestry reads each requirement's resource path from IAM
// when a request is opened (iam.ResourceRepository).
func WithResourceAncestry(a policy.ResourceAncestry) Option {
	return func(o *options) { o.ancestry = a }
}

// WithAgentSessions reads who a requesting agent Session acts for, and
// whether it was revoked (iam.AgentSessionRepository).
func WithAgentSessions(s policy.AgentSessions) Option { return func(o *options) { o.sessions = s } }

func WithAuthorizer(a authorization.Authorizer) Option { return func(o *options) { o.authz = a } }

// WithTransactor makes a request's change, its events, its audit record,
// and the change to its subject commit together.
func WithTransactor(tx transaction.Transactor) Option { return func(o *options) { o.writer.Tx = tx } }

func WithOutboxRecorder(r outbox.Recorder) Option { return func(o *options) { o.writer.Outbox = r } }

func WithAuditRecorder(r audit.Recorder) Option { return func(o *options) { o.writer.Audit = r } }

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

type dependency struct {
	name string
	ok   bool
}

// check reports a missing dependency of a service that writes.
func (o options) check(service string, required ...dependency) error {
	if err := requireAll(service, required...); err != nil {
		return err
	}
	if err := o.writer.Check(); err != nil {
		return fmt.Errorf("%s: %w", service, err)
	}
	return nil
}

func requireAll(service string, required ...dependency) error {
	for _, d := range required {
		if !d.ok {
			return fmt.Errorf("%s: %s is required", service, d.name)
		}
	}
	return nil
}

// NewApprovals returns the policy.Approvals that capabilities open and
// cancel requests through, or an error naming a missing dependency.
func NewApprovals(opts ...Option) (policy.Approvals, error) {
	o := newOptions(opts)
	if err := o.check("approvals",
		dependency{"ApprovalRequestRepo", o.requests != nil},
		dependency{"ResourceAncestry", o.ancestry != nil},
		dependency{"AgentSessions", o.sessions != nil},
	); err != nil {
		return nil, err
	}
	return &recorder{options: o}, nil
}

// NewService returns the ApprovalService, authorizing every operation, or
// an error naming a missing dependency.
func NewService(opts ...Option) (policy.ApprovalService, error) {
	o := newOptions(opts)
	if err := o.check("approval service",
		dependency{"ApprovalRequestRepo", o.requests != nil},
		dependency{"Subjects", o.subjects != nil},
		dependency{"HeldResources", o.held != nil},
		dependency{"AgentSessions", o.sessions != nil},
		dependency{"Authorizer", o.authz != nil},
	); err != nil {
		return nil, err
	}
	return &authorized{next: &service{options: o}, authz: o.authz, requests: o.requests}, nil
}

// save writes a request, its events, and its audit record, stamped with
// decision.
func (o options) save(ctx context.Context, actor identity.Actor, r *policy.ApprovalRequest, decision authorization.Decision,
	eventType string, before *policy.ApprovalRequestSnapshot) error {
	ctx = authorization.WithDecision(ctx, r.ID(), decision)
	if err := o.writer.Events(ctx, policy.AggregateApprovalRequest, r.ID(), r); err != nil {
		return err
	}
	e := audit.Event{Type: eventType, OrganizationID: r.OrganizationID(), ResourceID: r.ID(), Actor: actor, After: r.Snapshot()}
	if before != nil {
		e.Before = *before
	}
	return o.writer.AuditRecord(ctx, e)
}
