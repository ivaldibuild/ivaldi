// Package serviceaccounts manages service accounts and their API tokens:
// /v1/service-accounts and /v1/api-tokens. A service account is how CI and
// other integrations act in an Organization; it is granted roles like anyone
// else, and authenticates with API tokens (see package apitokens).
package serviceaccounts

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type ServiceOption func(*options)

type options struct {
	accounts iam.ServiceAccountRepository
	tokens   iam.APITokenRepository
	authz    authorization.Authorizer
	writer   change.Writer
}

func WithServiceAccountRepo(r iam.ServiceAccountRepository) ServiceOption {
	return func(o *options) { o.accounts = r }
}
func WithAPITokenRepo(r iam.APITokenRepository) ServiceOption {
	return func(o *options) { o.tokens = r }
}
func WithAuthorizer(a authorization.Authorizer) ServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor, WithOutboxRecorder, and WithAuditRecorder commit each
// change with its events and audit record.
func WithTransactor(tx transaction.Transactor) ServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewService returns the ServiceAccountService, which asks the authorizer
// before every operation. Every option is required.
func NewService(opts ...ServiceOption) (iam.ServiceAccountService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.accounts == nil:
		return nil, fmt.Errorf("service account service: ServiceAccountRepo is required")
	case o.tokens == nil:
		return nil, fmt.Errorf("service account service: APITokenRepo is required")
	case o.authz == nil:
		return nil, fmt.Errorf("service account service: Authorizer is required")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("service account service: %w", err)
	}
	return &authorized{next: &service{options: o}, authz: o.authz}, nil
}

type service struct {
	options
}

func (s *service) CreateServiceAccount(ctx context.Context, actor identity.Actor, in iam.CreateServiceAccountInput) (*iam.ServiceAccount, error) {
	sa, err := iam.NewServiceAccount(iam.NewServiceAccountProps{
		PrincipalProps:       iam.PrincipalProps{OrganizationID: in.OrganizationID, DisplayName: in.DisplayName},
		Description:          in.Description,
		CreatedByPrincipalID: actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.accounts.Write(ctx, sa); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateServiceAccount, sa.ID(), sa); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: iam.EventServiceAccountCreated, OrganizationID: in.OrganizationID, ResourceID: sa.ID(), Actor: actor, After: sa.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return sa, nil
}

func (s *service) ListServiceAccounts(ctx context.Context, _ identity.Actor, organizationID string, includeInactive bool) ([]*iam.ServiceAccount, error) {
	return s.accounts.List(ctx, organizationID, includeInactive)
}

func (s *service) GetServiceAccount(ctx context.Context, _ identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	return s.accounts.Get(ctx, organizationID, id)
}

func (s *service) SuspendServiceAccount(ctx context.Context, actor identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	return s.changeStatus(ctx, actor, organizationID, id, iam.EventServiceAccountSuspended, (*iam.ServiceAccount).Suspend)
}

func (s *service) ReactivateServiceAccount(ctx context.Context, actor identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	return s.changeStatus(ctx, actor, organizationID, id, iam.EventServiceAccountReactivated, (*iam.ServiceAccount).Reactivate)
}

func (s *service) changeStatus(ctx context.Context, actor identity.Actor, organizationID, id, event string, apply func(*iam.ServiceAccount) error) (*iam.ServiceAccount, error) {
	return change.Run(ctx, s.writer, actor, change.Spec[*iam.ServiceAccount]{
		AggregateType: iam.AggregateServiceAccount,
		AggregateID:   func(sa *iam.ServiceAccount) string { return sa.ID() },
		Load:          func(ctx context.Context) (*iam.ServiceAccount, error) { return s.accounts.Get(ctx, organizationID, id) },
		Apply:         func(_ context.Context, sa *iam.ServiceAccount) error { return apply(sa) },
		Save:          s.accounts.Write,
		Snapshot:      func(sa *iam.ServiceAccount) any { return sa.Snapshot() },
		Event: func(sa *iam.ServiceAccount) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: sa.ID()}
		},
	})
}

func (s *service) CreateAPIToken(ctx context.Context, actor identity.Actor, in iam.CreateAPITokenInput) (*iam.APIToken, string, error) {
	issued, err := iam.NewAPITokenSecret()
	if err != nil {
		return nil, "", err
	}
	var token *iam.APIToken
	err = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		sa, err := s.accounts.Get(ctx, in.OrganizationID, in.ServiceAccountID)
		if err != nil {
			return err
		}
		if !sa.Active() {
			return fmt.Errorf("%w: service account %s is %s", iam.ErrStatus, sa.ID(), sa.Status())
		}
		token, err = iam.NewAPIToken(iam.NewAPITokenProps{
			OrganizationID: in.OrganizationID, PrincipalID: sa.ID(), Name: in.Name,
			TokenPrefix: issued.Prefix, TokenHash: issued.Hash, ExpiresAt: in.ExpiresAt,
		}, nil)
		if err != nil {
			return err
		}
		if err := s.tokens.Write(ctx, token); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateAPIToken, token.ID(), token); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: iam.EventAPITokenCreated, OrganizationID: in.OrganizationID, ResourceID: token.ID(), Actor: actor, After: token.Snapshot(),
		})
	})
	if err != nil {
		return nil, "", err
	}
	return token, issued.Secret, nil
}

func (s *service) ListAPITokens(ctx context.Context, _ identity.Actor, organizationID, id string) ([]*iam.APIToken, error) {
	if _, err := s.accounts.Get(ctx, organizationID, id); err != nil {
		return nil, err
	}
	return s.tokens.ListByPrincipal(ctx, organizationID, id)
}

func (s *service) RevokeAPIToken(ctx context.Context, actor identity.Actor, organizationID, tokenID string) (*iam.APIToken, error) {
	return change.Run(ctx, s.writer, actor, change.Spec[*iam.APIToken]{
		AggregateType: iam.AggregateAPIToken,
		AggregateID:   func(t *iam.APIToken) string { return t.ID() },
		Load:          func(ctx context.Context) (*iam.APIToken, error) { return s.tokens.Get(ctx, organizationID, tokenID) },
		Apply: func(_ context.Context, t *iam.APIToken) error {
			if t.Revoked() {
				return change.ErrUnchanged
			}
			t.Revoke()
			return nil
		},
		Save:     s.tokens.Write,
		Snapshot: func(t *iam.APIToken) any { return t.Snapshot() },
		Event: func(t *iam.APIToken) audit.Event {
			return audit.Event{Type: iam.EventAPITokenRevoked, OrganizationID: organizationID, ResourceID: t.ID()}
		},
	})
}

// authorized checks every operation on the Organization: reading needs
// service_account.read, and creating, suspending, reactivating, and issuing
// or revoking tokens need service_account.manage.
type authorized struct {
	next  iam.ServiceAccountService
	authz authorization.Authorizer
}

var _ iam.ServiceAccountService = (*authorized)(nil)

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, action, organizationID string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) CreateServiceAccount(ctx context.Context, actor identity.Actor, in iam.CreateServiceAccountInput) (*iam.ServiceAccount, error) {
	ctx, err := a.decide(ctx, actor, iam.PermissionServiceAccountManage, iam.ActionServiceAccountCreate, in.OrganizationID, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateServiceAccount(ctx, actor, in)
}

func (a *authorized) ListServiceAccounts(ctx context.Context, actor identity.Actor, organizationID string, includeInactive bool) ([]*iam.ServiceAccount, error) {
	if _, err := a.decide(ctx, actor, iam.PermissionServiceAccountRead, iam.ActionServiceAccountList, organizationID, nil); err != nil {
		return nil, err
	}
	return a.next.ListServiceAccounts(ctx, actor, organizationID, includeInactive)
}

func (a *authorized) GetServiceAccount(ctx context.Context, actor identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	if _, err := a.decide(ctx, actor, iam.PermissionServiceAccountRead, iam.ActionServiceAccountRead, organizationID, nil); err != nil {
		return nil, err
	}
	return a.next.GetServiceAccount(ctx, actor, organizationID, id)
}

func (a *authorized) SuspendServiceAccount(ctx context.Context, actor identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	ctx, err := a.decide(ctx, actor, iam.PermissionServiceAccountManage, iam.ActionServiceAccountSuspend, organizationID, id)
	if err != nil {
		return nil, err
	}
	return a.next.SuspendServiceAccount(ctx, actor, organizationID, id)
}

func (a *authorized) ReactivateServiceAccount(ctx context.Context, actor identity.Actor, organizationID, id string) (*iam.ServiceAccount, error) {
	ctx, err := a.decide(ctx, actor, iam.PermissionServiceAccountManage, iam.ActionServiceAccountReactivate, organizationID, id)
	if err != nil {
		return nil, err
	}
	return a.next.ReactivateServiceAccount(ctx, actor, organizationID, id)
}

func (a *authorized) CreateAPIToken(ctx context.Context, actor identity.Actor, in iam.CreateAPITokenInput) (*iam.APIToken, string, error) {
	ctx, err := a.decide(ctx, actor, iam.PermissionServiceAccountManage, iam.ActionAPITokenCreate, in.OrganizationID, in)
	if err != nil {
		return nil, "", err
	}
	return a.next.CreateAPIToken(ctx, actor, in)
}

func (a *authorized) ListAPITokens(ctx context.Context, actor identity.Actor, organizationID, id string) ([]*iam.APIToken, error) {
	if _, err := a.decide(ctx, actor, iam.PermissionServiceAccountRead, iam.ActionAPITokenList, organizationID, nil); err != nil {
		return nil, err
	}
	return a.next.ListAPITokens(ctx, actor, organizationID, id)
}

func (a *authorized) RevokeAPIToken(ctx context.Context, actor identity.Actor, organizationID, tokenID string) (*iam.APIToken, error) {
	ctx, err := a.decide(ctx, actor, iam.PermissionServiceAccountManage, iam.ActionAPITokenRevoke, organizationID, tokenID)
	if err != nil {
		return nil, err
	}
	return a.next.RevokeAPIToken(ctx, actor, organizationID, tokenID)
}
