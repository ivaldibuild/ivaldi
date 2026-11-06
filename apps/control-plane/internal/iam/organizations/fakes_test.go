package organizations

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// store fakes the Organization repository and the transactor. It keeps
// snapshots and rebuilds Organizations from them, as the PostgreSQL
// repository does, and a failed transaction puts them back. It embeds the
// repository interface, so a call to a method it does not fake panics. Its
// auditLog and outboxLog fake the recorders.
type store struct {
	iam.OrganizationRepository

	orgs   map[string]iam.OrganizationSnapshot
	audit  []audit.Event
	events []events.Envelope
	// failWrite makes Write fail with the error.
	failWrite error
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) load(id string) (*iam.Organization, error) {
	snap, ok := s.orgs[id]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return iam.NewOrganization(iam.NewOrganizationProps{
		Slug: snap.Slug, Name: snap.Name, DisplayName: snap.DisplayName, LogoURL: snap.LogoURL,
		Status: snap.Status, Metadata: snap.Metadata, CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt,
	}, &snap.ID)
}

func (s *store) GetByIdentifier(_ context.Context, identifier string) (*iam.Organization, error) {
	for id, snap := range s.orgs {
		if id == identifier || snap.Slug == identifier {
			return s.load(id)
		}
	}
	return nil, iam.ErrNotFound
}

func (s *store) GetForUpdate(_ context.Context, organizationID string) (*iam.Organization, error) {
	return s.load(organizationID)
}

func (s *store) Write(_ context.Context, o *iam.Organization) error {
	if s.failWrite != nil {
		return s.failWrite
	}
	for id, snap := range s.orgs {
		if id != o.ID() && snap.Slug == o.Slug() {
			return iam.ErrAlreadyExists
		}
	}
	s.orgs[o.ID()] = o.Snapshot()
	return nil
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	orgs, auditLen, eventsLen := maps.Clone(s.orgs), len(s.audit), len(s.events)
	if err := fn(ctx); err != nil {
		s.orgs, s.audit, s.events = orgs, s.audit[:auditLen], s.events[:eventsLen]
		return err
	}
	return nil
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

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's action.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
		d.Reason = "denied by the test"
	}
	return d, nil
}

// world is the Acme Organization and another one, Globex, with the clock
// fixed, and Ada acting.
type world struct {
	*store
	svc   iam.OrganizationService
	authz *authorizer
	acme  string
	other string
	ada   identity.Actor
}

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	acme := must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", Name: "Acme Corporation", DisplayName: "Acme"}, nil))
	globex := must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "globex", DisplayName: "Globex"}, nil))
	s := &store{orgs: map[string]iam.OrganizationSnapshot{acme.ID(): acme.Snapshot(), globex.ID(): globex.Snapshot()}}
	authz := &authorizer{}
	svc, err := NewOrganizationService(s.options(authz)...)
	require.NoError(t, err)
	return &world{
		store: s, svc: svc, authz: authz, acme: acme.ID(), other: globex.ID(),
		ada: identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser},
	}
}

func (s *store) options(authz authorization.Authorizer) []OrganizationServiceOption {
	return []OrganizationServiceOption{
		WithOrganizationRepo(s), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}
