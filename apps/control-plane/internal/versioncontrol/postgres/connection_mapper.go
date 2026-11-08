package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type connectionMapper struct{}

func newConnectionMapper() *connectionMapper { return &connectionMapper{} }

func (m *connectionMapper) ToPersistence(c *versioncontrol.Connection) *connectionEntity {
	s := c.Snapshot()
	return &connectionEntity{
		ID:                   s.ID,
		OrganizationID:       s.OrganizationID,
		Provider:             string(s.Provider),
		HostURL:              s.HostURL,
		AuthKind:             string(s.AuthKind),
		CredentialRef:        s.CredentialRef,
		ExternalAccountID:    s.ExternalAccountID,
		ExternalAccountName:  s.ExternalAccountName,
		LookupFrom:           string(s.LookupFrom),
		LookupPool:           s.LookupPool,
		Status:               string(s.Status),
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            s.CreatedAt,
		UpdatedAt:            s.UpdatedAt,
		DeletedAt:            s.DeletedAt,
	}
}

func (m *connectionMapper) ToDomain(e *connectionEntity) (*versioncontrol.Connection, error) {
	props := versioncontrol.NewConnectionProps{
		OrganizationID:       e.OrganizationID,
		Provider:             vcs.ProviderKind(e.Provider),
		HostURL:              e.HostURL,
		AuthKind:             versioncontrol.AuthKind(e.AuthKind),
		CredentialRef:        e.CredentialRef,
		ExternalAccountID:    e.ExternalAccountID,
		ExternalAccountName:  e.ExternalAccountName,
		LookupFrom:           vcs.LookupFrom(e.LookupFrom),
		LookupPool:           e.LookupPool,
		Status:               versioncontrol.Status(e.Status),
		CreatedByPrincipalID: e.CreatedByPrincipalID,
		CreatedAt:            utc(e.CreatedAt),
		UpdatedAt:            utc(e.UpdatedAt),
		DeletedAt:            utcPtr(e.DeletedAt),
	}
	return versioncontrol.NewConnection(props, &e.ID)
}
