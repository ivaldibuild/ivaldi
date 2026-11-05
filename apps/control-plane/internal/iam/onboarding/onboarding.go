package onboarding

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type OnboardingServiceOption func(*onboardingService)

type onboardingService struct {
	logger                 logging.Logger
	organizationRepo       iam.OrganizationRepository
	accountRepo            iam.AccountRepository
	passwordCredentialRepo iam.PasswordCredentialRepository
	userRepo               iam.UserRepository
	tx                     transaction.Transactor
	outbox                 outbox.Recorder
	passwords              hasher.PasswordHasher
}

func WithLogger(logger logging.Logger) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.logger = logger
	}
}

func WithOrganizationRepo(repo iam.OrganizationRepository) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.organizationRepo = repo
	}
}

func WithAccountRepo(repo iam.AccountRepository) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.accountRepo = repo
	}
}

func WithPasswordCredentialRepo(repo iam.PasswordCredentialRepository) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.passwordCredentialRepo = repo
	}
}

func WithUserRepo(repo iam.UserRepository) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.userRepo = repo
	}
}

// WithTransactor makes onboarding's writes and events commit together, so
// the repositories and the Outbox Recorder must write through the context it
// passes.
func WithTransactor(tx transaction.Transactor) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.tx = tx
	}
}

// WithOutboxRecorder records the events onboarding raises in its transaction.
func WithOutboxRecorder(o outbox.Recorder) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.outbox = o
	}
}

// WithPasswordHasher hashes the owner's password.
func WithPasswordHasher(h hasher.PasswordHasher) OnboardingServiceOption {
	return func(s *onboardingService) {
		s.passwords = h
	}
}

// NewOnboardingService returns the Onboarding service, or an error naming a
// missing dependency.
func NewOnboardingService(opts ...OnboardingServiceOption) (iam.Onboarding, error) {
	s := &onboardingService{}
	for _, opt := range opts {
		opt(s)
	}

	required := []struct {
		name string
		ok   bool
	}{
		{"Logger", s.logger != nil},
		{"OrganizationRepo", s.organizationRepo != nil},
		{"AccountRepo", s.accountRepo != nil},
		{"PasswordCredentialRepo", s.passwordCredentialRepo != nil},
		{"UserRepo", s.userRepo != nil},
		{"Transactor", s.tx != nil},
		{"Outbox", s.outbox != nil},
		{"PasswordHasher", s.passwords != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("onboarding service: %s is required", r.name)
		}
	}
	return s, nil
}

// OnboardOrganization creates an Organization with its owner: an Account, the
// Account's password, and the Account's User in the Organization. It creates
// all of them and their events, or nothing.
func (s *onboardingService) OnboardOrganization(ctx context.Context, input *iam.OnboardOrganizationInput) error {
	if err := input.Validate(); err != nil {
		return err
	}

	orgProps := iam.NewOrganizationProps{
		Name:        input.OrganizationName,
		DisplayName: input.OrganizationDisplayName,
		LogoURL:     input.OrganizationLogoURL,
	}
	if input.OrganizationSlug != nil {
		orgProps.Slug = *input.OrganizationSlug
	}
	org, err := iam.NewOrganization(orgProps, nil)
	if err != nil {
		return err
	}

	account, err := iam.NewAccount(iam.NewAccountProps{
		Email:       input.OwnerEmail,
		DisplayName: input.OwnerDisplayName,
	}, nil)
	if err != nil {
		return err
	}

	user, err := iam.NewUser(iam.NewUserProps{
		OrganizationID: org.ID(),
		DisplayName:    input.OwnerDisplayName,
		AccountID:      account.ID(),
	}, nil)
	if err != nil {
		return err
	}

	// Conflicts are checked before hashing, which is deliberately expensive.
	if err := s.assertAccountExistence(ctx, input.OwnerEmail); err != nil {
		return err
	}
	if err := s.assertOrganizationExistence(ctx, org.Name(), org.Slug()); err != nil {
		return err
	}

	passwordHash, err := s.passwords.Hash(input.OwnerPassword)
	if err != nil {
		s.logger.Error("failed to hash password", logging.F("error", err))
		return err
	}
	passwordCredential, err := iam.NewPasswordCredential(iam.NewPasswordCredentialProps{
		AccountID:    account.ID(),
		PasswordHash: passwordHash,
	})
	if err != nil {
		return err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// The checks above can race another onboarding; the unique indexes
		// on slugs and emails settle it.
		if err := s.organizationRepo.Write(ctx, org); err != nil {
			if errors.Is(err, iam.ErrAlreadyExists) {
				return NewErrOrganizationConflict("organization with this slug already exists")
			}
			return fmt.Errorf("write organization: %w", err)
		}
		if err := s.accountRepo.Write(ctx, account); err != nil {
			if errors.Is(err, iam.ErrAlreadyExists) {
				return NewErrAccountConflict("account with this email already exists")
			}
			return fmt.Errorf("write account: %w", err)
		}
		if err := s.passwordCredentialRepo.Write(ctx, passwordCredential); err != nil {
			return fmt.Errorf("write password credential: %w", err)
		}
		if err := s.userRepo.Write(ctx, user); err != nil {
			return fmt.Errorf("write user: %w", err)
		}

		// Accounts and their passwords belong to no Organization; their
		// events here are the Organization's onboarding.
		sources := []struct {
			aggregateType string
			aggregateID   string
			obj           interface{ PullEvents() []aggregate.Event }
		}{
			{iam.AggregateOrganization, org.ID(), org},
			{iam.AggregateAccount, account.ID(), account},
			{iam.AggregatePasswordCredential, account.ID(), passwordCredential},
			{iam.AggregateUser, user.ID(), user},
		}
		for _, src := range sources {
			if err := s.recordEvents(ctx, org.ID(), src.aggregateType, src.aggregateID, src.obj.PullEvents()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_, orgConflict := errors.AsType[*ErrOrganizationConflict](err)
		_, accountConflict := errors.AsType[*ErrAccountConflict](err)
		if !orgConflict && !accountConflict {
			s.logger.Error("failed to onboard organization", logging.F("error", err))
		}
		return err
	}
	return nil
}

// recordEvents writes the events to the outbox in the caller's transaction.
func (s *onboardingService) recordEvents(ctx context.Context, organizationID, aggregateType, aggregateID string, raised []aggregate.Event) error {
	envelopes, err := events.Wrap(ctx, events.Source{
		OrganizationID: organizationID,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
	}, raised...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", aggregateType, err)
	}
	if err := s.outbox.Record(ctx, envelopes...); err != nil {
		return fmt.Errorf("record %s events: %w", aggregateType, err)
	}
	return nil
}

// assertOrganizationExistence checks that no Organization has the name or slug.
func (s *onboardingService) assertOrganizationExistence(ctx context.Context, name, slug string) error {
	exists, err := s.organizationRepo.ExistsByName(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return NewErrOrganizationConflict("organization with this name already exists")
	}

	exists, err = s.organizationRepo.ExistsBySlug(ctx, slug)
	if err != nil {
		return err
	}
	if exists {
		return NewErrOrganizationConflict("organization with this slug already exists")
	}
	return nil
}

func (s *onboardingService) assertAccountExistence(ctx context.Context, email string) error {
	exists, err := s.accountRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return err
	}
	if exists {
		return NewErrAccountConflict("account with this email already exists")
	}
	return nil
}
