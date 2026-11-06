package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// principalRow returns the principals row of a principal of any kind.
func principalRow(s iam.PrincipalSnapshot) (*principalEntity, error) {
	metadata, err := encodeMetadata(s.Metadata)
	if err != nil {
		return nil, err
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
	}, nil
}

// principalProps returns what a kind's New… constructor needs from its
// principals row.
func principalProps(p *principalEntity) (iam.PrincipalProps, error) {
	metadata, err := decodeMetadata(p.Metadata)
	if err != nil {
		return iam.PrincipalProps{}, err
	}
	return iam.PrincipalProps{
		OrganizationID: p.OrganizationID,
		DisplayName:    p.DisplayName,
		Status:         iam.PrincipalStatus(p.Status),
		Metadata:       metadata,
		CreatedAt:      utc(p.CreatedAt),
		UpdatedAt:      utc(p.UpdatedAt),
	}, nil
}

// principalSnapshot returns the state a principals row holds, whatever its
// kind.
func principalSnapshot(p *principalEntity) (iam.PrincipalSnapshot, error) {
	metadata, err := decodeMetadata(p.Metadata)
	if err != nil {
		return iam.PrincipalSnapshot{}, err
	}
	return iam.PrincipalSnapshot{
		ID:             p.ID,
		OrganizationID: p.OrganizationID,
		Kind:           iam.PrincipalKind(p.Kind),
		DisplayName:    p.DisplayName,
		Status:         iam.PrincipalStatus(p.Status),
		Metadata:       metadata,
		CreatedAt:      *utc(p.CreatedAt),
		UpdatedAt:      *utc(p.UpdatedAt),
	}, nil
}
