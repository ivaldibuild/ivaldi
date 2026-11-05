package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type externalIdentityMapper struct{}

func newExternalIdentityMapper() *externalIdentityMapper {
	return &externalIdentityMapper{}
}

func (m *externalIdentityMapper) ToPersistence(x *iam.ExternalIdentity) *externalIdentityEntity {
	s := x.Snapshot()
	return &externalIdentityEntity{
		ID:                 s.ID,
		OrganizationID:     s.OrganizationID,
		PrincipalID:        s.PrincipalID,
		IdentityProviderID: s.IdentityProviderID,
		Subject:            s.Subject,
		LastLoginAt:        s.LastLoginAt,
		CreatedAt:          s.CreatedAt,
	}
}

func (m *externalIdentityMapper) ToDomain(e *externalIdentityEntity) (*iam.ExternalIdentity, error) {
	return iam.NewExternalIdentity(iam.NewExternalIdentityProps{
		OrganizationID:     e.OrganizationID,
		PrincipalID:        e.PrincipalID,
		IdentityProviderID: e.IdentityProviderID,
		Subject:            e.Subject,
		LastLoginAt:        utcPtr(e.LastLoginAt),
		CreatedAt:          utc(e.CreatedAt),
	}, &e.ID)
}

type accountIdentityMapper struct{}

func newAccountIdentityMapper() *accountIdentityMapper {
	return &accountIdentityMapper{}
}

func (m *accountIdentityMapper) ToPersistence(a *iam.AccountIdentity) *accountIdentityEntity {
	s := a.Snapshot()
	return &accountIdentityEntity{
		ID:          s.ID,
		AccountID:   s.AccountID,
		Provider:    string(s.Provider),
		Subject:     s.Subject,
		LastLoginAt: s.LastLoginAt,
		CreatedAt:   s.CreatedAt,
	}
}

func (m *accountIdentityMapper) ToDomain(e *accountIdentityEntity) (*iam.AccountIdentity, error) {
	return iam.NewAccountIdentity(iam.NewAccountIdentityProps{
		AccountID:   e.AccountID,
		Provider:    iam.PlatformProvider(e.Provider),
		Subject:     e.Subject,
		LastLoginAt: utcPtr(e.LastLoginAt),
		CreatedAt:   utc(e.CreatedAt),
	}, &e.ID)
}
