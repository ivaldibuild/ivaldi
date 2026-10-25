package iam

// Events an OrganizationDomain records. Each carries the OrganizationDomain
// as it is after the change, without its verification token hash.

// OrganizationDomainCreated is recorded when an Organization claims a
// domain.
type OrganizationDomainCreated struct {
	OrganizationDomain OrganizationDomainSnapshot `json:"organization_domain"`
}

// OrganizationDomainRechallenged is recorded when a domain gets a new
// verification token and becomes unverified until it is found.
type OrganizationDomainRechallenged struct {
	OrganizationDomain OrganizationDomainSnapshot `json:"organization_domain"`
}

// OrganizationDomainVerified is recorded when a domain's verification token
// is found in DNS.
type OrganizationDomainVerified struct {
	OrganizationDomain OrganizationDomainSnapshot `json:"organization_domain"`
}

func (OrganizationDomainCreated) EventName() string { return "iam.v1.organization_domain.created" }
func (OrganizationDomainRechallenged) EventName() string {
	return "iam.v1.organization_domain.rechallenged"
}
func (OrganizationDomainVerified) EventName() string { return "iam.v1.organization_domain.verified" }
