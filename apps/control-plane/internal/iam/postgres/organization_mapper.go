package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type organizationMapper struct{}

func newOrganizationMapper() *organizationMapper {
	return &organizationMapper{}
}

func (m *organizationMapper) ToPersistence(o *iam.Organization) (*organizationEntity, error) {
	s := o.Snapshot()
	metadata, err := encodeMetadata(s.Metadata)
	if err != nil {
		return nil, err
	}
	return &organizationEntity{
		ID:          s.ID,
		Slug:        s.Slug,
		Name:        s.Name,
		DisplayName: s.DisplayName,
		LogoURL:     s.LogoURL,
		Status:      string(s.Status),
		Metadata:    metadata,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}, nil
}

func (m *organizationMapper) ToDomain(e *organizationEntity) (*iam.Organization, error) {
	metadata, err := decodeMetadata(e.Metadata)
	if err != nil {
		return nil, err
	}
	return iam.NewOrganization(iam.NewOrganizationProps{
		Slug:        e.Slug,
		Name:        e.Name,
		DisplayName: e.DisplayName,
		LogoURL:     e.LogoURL,
		Status:      iam.OrganizationStatus(e.Status),
		Metadata:    metadata,
		CreatedAt:   utc(e.CreatedAt),
		UpdatedAt:   utc(e.UpdatedAt),
	}, &e.ID)
}
