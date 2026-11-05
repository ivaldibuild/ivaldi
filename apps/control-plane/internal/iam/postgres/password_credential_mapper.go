package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type passwordCredentialMapper struct{}

func newPasswordCredentialMapper() *passwordCredentialMapper {
	return &passwordCredentialMapper{}
}

func (m *passwordCredentialMapper) ToPersistence(c *iam.PasswordCredential) *passwordCredentialEntity {
	s := c.Snapshot()
	return &passwordCredentialEntity{
		AccountID:    s.AccountID,
		PasswordHash: s.PasswordHash,
		UpdatedAt:    s.UpdatedAt,
	}
}

func (m *passwordCredentialMapper) ToDomain(e *passwordCredentialEntity) (*iam.PasswordCredential, error) {
	return iam.NewPasswordCredential(iam.NewPasswordCredentialProps{
		AccountID:    e.AccountID,
		PasswordHash: e.PasswordHash,
		UpdatedAt:    utc(e.UpdatedAt),
	})
}
