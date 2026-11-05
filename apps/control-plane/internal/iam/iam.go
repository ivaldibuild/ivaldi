// Package iam is the control plane's identity and access capability (ADR
// 0003, docs/design/iam-data-model.md): Organizations and the people,
// integrations, agent Sessions, and workloads that act in them, how they
// sign in, and the grants that decide what they may touch.
//
// So far it holds the domain objects and the contracts other capabilities
// depend on. Services and repositories follow.
package iam

import "github.com/ivaldibuild/ivaldi/pkg/id"

func init() {
	id.MustRegister(Organization{}.Prefix(), "iam.Organization")
	id.MustRegister(Account{}.Prefix(), "iam.Account")
	// Users, service accounts, agent Sessions, and workloads share the ID of
	// their principals row.
	id.MustRegister(principalPrefix, "iam.Principal")
	id.MustRegister(IdentityProvider{}.Prefix(), "iam.IdentityProvider")
	id.MustRegister(ExternalIdentity{}.Prefix(), "iam.ExternalIdentity")
	id.MustRegister(AccountIdentity{}.Prefix(), "iam.AccountIdentity")
	id.MustRegister(APIToken{}.Prefix(), "iam.APIToken")
	id.MustRegister(WorkloadEnrollmentToken{}.Prefix(), "iam.WorkloadEnrollmentToken")
	id.MustRegister(WorkloadCredential{}.Prefix(), "iam.WorkloadCredential")
	id.MustRegister(OAuthClient{}.Prefix(), "iam.OAuthClient")
	id.MustRegister(OAuthRefreshToken{}.Prefix(), "iam.OAuthRefreshToken")
	id.MustRegister(Role{}.Prefix(), "iam.Role")
	id.MustRegister(Group{}.Prefix(), "iam.Group")
	id.MustRegister(Grant{}.Prefix(), "iam.Grant")
}
