package connections

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type CreateConnectionRequest struct {
	Body struct {
		Provider            string  `json:"provider" required:"true" enum:"github,gitlab,bitbucket,gitea,azure_devops" doc:"The version-control provider"`
		HostURL             string  `json:"host_url" required:"true" format:"uri" doc:"The provider's address, such as https://github.com or a self-hosted server"`
		AuthKind            string  `json:"auth_kind,omitempty" required:"false" enum:"app_installation,oauth,access_token" doc:"How Ivaldi authenticates; app_installation when omitted"`
		ExternalAccountID   string  `json:"external_account_id" required:"true" doc:"The provider's account or installation ID"`
		ExternalAccountName *string `json:"external_account_name,omitempty" required:"false" doc:"The provider account's name, such as the GitHub organization login"`
		LookupFrom          string  `json:"lookup_from,omitempty" required:"false" enum:"control_plane,data_plane" doc:"Where lookups run; control_plane when omitted"`
		LookupPool          *string `json:"lookup_pool,omitempty" required:"false" doc:"The data-plane pool that runs lookups, with lookup_from data_plane"`
	}
}

func (r *CreateConnectionRequest) input(organizationID string) versioncontrol.CreateConnectionInput {
	authKind := versioncontrol.AuthKind(r.Body.AuthKind)
	if authKind == "" {
		authKind = versioncontrol.AuthKindAppInstallation
	}
	lookupFrom := vcs.LookupFrom(r.Body.LookupFrom)
	if lookupFrom == "" {
		lookupFrom = vcs.LookupFromControlPlane
	}
	return versioncontrol.CreateConnectionInput{
		OrganizationID:      organizationID,
		Provider:            vcs.ProviderKind(r.Body.Provider),
		HostURL:             r.Body.HostURL,
		AuthKind:            authKind,
		ExternalAccountID:   r.Body.ExternalAccountID,
		ExternalAccountName: r.Body.ExternalAccountName,
		LookupFrom:          lookupFrom,
		LookupPool:          r.Body.LookupPool,
	}
}

type ListConnectionsRequest struct {
	IncludeArchived bool `query:"include_archived" doc:"Include archived connections"`
}

type ConnectionRequest struct {
	ConnectionID string `path:"connection_id" doc:"The Connection's ID"`
}
