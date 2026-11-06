// Package people lists an Organization's people: GET /v1/people.
package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// maxSearchLength caps a search, which matches names and emails.
const maxSearchLength = 320

type PeopleServiceOption func(*options)

type options struct {
	people iam.PeopleRepository
	authz  authorization.Authorizer
}

func WithPeopleRepo(r iam.PeopleRepository) PeopleServiceOption {
	return func(o *options) { o.people = r }
}

func WithAuthorizer(a authorization.Authorizer) PeopleServiceOption {
	return func(o *options) { o.authz = a }
}

// NewPeopleService returns the PeopleService, which asks the authorizer
// before every operation. Every option is required.
func NewPeopleService(opts ...PeopleServiceOption) (iam.PeopleService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.people == nil:
		return nil, errors.New("people service: PeopleRepo is required")
	case o.authz == nil:
		return nil, errors.New("people service: Authorizer is required")
	}
	return &authorized{next: &service{people: o.people}, authz: o.authz}, nil
}

// authorized checks user.read on the Organization before listing.
type authorized struct {
	next  iam.PeopleService
	authz authorization.Authorizer
}

var _ iam.PeopleService = (*authorized)(nil)

func (a *authorized) ListPeople(ctx context.Context, actor identity.Actor, q iam.ListPeopleQuery) (*pagination.Result[iam.Person], error) {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionUserRead, OrganizationID: q.OrganizationID,
		ResourceID: q.OrganizationID, Action: iam.ActionPeopleList,
	})
	if err != nil {
		return nil, err
	}
	return a.next.ListPeople(ctx, actor, q)
}

type service struct {
	people iam.PeopleRepository
}

func (s *service) ListPeople(ctx context.Context, _ identity.Actor, q iam.ListPeopleQuery) (*pagination.Result[iam.Person], error) {
	if len(q.Search) > maxSearchLength {
		return nil, iam.NewErrInvalidInput(fmt.Sprintf("the search must be at most %d characters", maxSearchLength))
	}
	return s.people.ListPeople(ctx, q)
}
