package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type userMapper struct{}

func newUserMapper() *userMapper {
	return &userMapper{}
}

// ToPersistence returns the User's principals row and its users row.
func (m *userMapper) ToPersistence(u *iam.User) (*principalEntity, *userEntity, error) {
	s := u.Snapshot()
	metadata, err := encodeMetadata(s.Metadata)
	if err != nil {
		return nil, nil, err
	}
	return &principalEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		Kind:           string(s.Kind),
		DisplayName:    s.DisplayName,
		Status:         string(s.Status),
		Metadata:       metadata,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}, &userEntity{
		PrincipalID:    s.ID,
		OrganizationID: s.OrganizationID,
		AccountID:      s.AccountID,
		JoinedAt:       s.JoinedAt,
		LastSignedInAt: s.LastSignedInAt,
	}, nil
}

func (m *userMapper) ToDomain(p *principalEntity, u *userEntity) (*iam.User, error) {
	metadata, err := decodeMetadata(p.Metadata)
	if err != nil {
		return nil, err
	}
	return iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{
			OrganizationID: p.OrganizationID,
			DisplayName:    p.DisplayName,
			Status:         iam.PrincipalStatus(p.Status),
			Metadata:       metadata,
			CreatedAt:      utc(p.CreatedAt),
			UpdatedAt:      utc(p.UpdatedAt),
		},
		AccountID:      u.AccountID,
		JoinedAt:       utc(u.JoinedAt),
		LastSignedInAt: utcPtr(u.LastSignedInAt),
	}, &p.ID)
}
