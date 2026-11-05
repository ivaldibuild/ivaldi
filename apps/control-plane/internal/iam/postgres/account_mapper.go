package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type accountMapper struct{}

func newAccountMapper() *accountMapper {
	return &accountMapper{}
}

func (m *accountMapper) ToPersistence(a *iam.Account) *accountEntity {
	s := a.Snapshot()
	return &accountEntity{
		ID:              s.ID,
		Email:           s.Email,
		EmailVerifiedAt: s.EmailVerifiedAt,
		DisplayName:     s.DisplayName,
		Status:          string(s.Status),
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func (m *accountMapper) ToDomain(e *accountEntity) (*iam.Account, error) {
	return iam.NewAccount(iam.NewAccountProps{
		Email:           e.Email,
		EmailVerifiedAt: utcPtr(e.EmailVerifiedAt),
		DisplayName:     e.DisplayName,
		Status:          iam.AccountStatus(e.Status),
		CreatedAt:       utc(e.CreatedAt),
		UpdatedAt:       utc(e.UpdatedAt),
	}, &e.ID)
}
