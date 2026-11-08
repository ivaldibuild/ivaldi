package changes

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// A Change belongs to a Source, so every operation is checked on the Source.
// ChangesForCommits does not authorize: the caller has already authorized
// its own read, as Lookup documents.
type authorized struct {
	next  provenance.ChangeService
	authz authorization.Authorizer
}

var _ provenance.ChangeService = (*authorized)(nil)

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, organizationID, sourceID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: sourceID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) RecordChange(ctx context.Context, actor identity.Actor, in provenance.RecordChangeInput) (*provenance.Change, error) {
	ctx, err := a.decide(ctx, actor, provenance.PermissionChangeRecord, in.OrganizationID, in.SourceID, provenance.ActionChangeRecord, in)
	if err != nil {
		return nil, err
	}
	return a.next.RecordChange(ctx, actor, in)
}

func (a *authorized) GetChange(ctx context.Context, actor identity.Actor, organizationID, changeID string) (*provenance.Change, error) {
	c, err := a.next.GetChange(ctx, actor, organizationID, changeID)
	if err != nil {
		return nil, err
	}
	if _, err := a.decide(ctx, actor, provenance.PermissionChangeRead, organizationID, c.SourceID(), provenance.ActionChangeRead, nil); err != nil {
		return nil, err
	}
	return c, nil
}

func (a *authorized) ListChanges(ctx context.Context, actor identity.Actor, query provenance.ListChangesQuery) (*pagination.Result[*provenance.Change], error) {
	if _, err := a.decide(ctx, actor, provenance.PermissionChangeRead, query.OrganizationID, query.SourceID, provenance.ActionChangeList, nil); err != nil {
		return nil, err
	}
	return a.next.ListChanges(ctx, actor, query)
}

func (a *authorized) ChangesForCommits(ctx context.Context, organizationID, sourceID string, commitSHAs []string) ([]*provenance.Change, error) {
	return a.next.ChangesForCommits(ctx, organizationID, sourceID, commitSHAs)
}
