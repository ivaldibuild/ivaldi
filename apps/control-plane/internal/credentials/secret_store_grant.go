package credentials

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// SecretStoreGrant records access the control plane granted to a Secret
// Store, for one Deployment (ADR 0017). It never holds what was granted:
// for a federated grant, it names the JWT by its ID. Grants raise no domain
// event; each has an audit record.
type SecretStoreGrant struct {
	aggregate.Root
	organizationID string
	storeID        string
	deploymentID   string
	mode           AccessMode
	tokenID        string
	expiresAt      time.Time
	revokedAt      *time.Time
	createdAt      time.Time
}

// SecretStoreGrantSnapshot is a grant's state, for audit records.
type SecretStoreGrantSnapshot struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	StoreID        string     `json:"secret_store_id"`
	DeploymentID   string     `json:"deployment_id"`
	Mode           AccessMode `json:"mode" enum:"agent,federated"`
	// TokenID identifies what was granted, such as a federated JWT's jti,
	// never the credential itself.
	TokenID   string     `json:"token_id"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// NewSecretStoreGrantProps describes a grant. A new one leaves CreatedAt
// nil; a repository loading a stored one sets it.
type NewSecretStoreGrantProps struct {
	OrganizationID string
	StoreID        string
	DeploymentID   string
	Mode           AccessMode
	TokenID        string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	CreatedAt      *time.Time
}

// NewSecretStoreGrant returns a grant, with a new ID unless one is given.
func NewSecretStoreGrant(props NewSecretStoreGrantProps, grantID *string) (*SecretStoreGrant, error) {
	switch {
	case props.OrganizationID == "", props.StoreID == "", props.DeploymentID == "":
		return nil, NewErrInvalidInput("a grant names its organization, secret store, and deployment")
	case props.Mode != AccessFederated:
		return nil, NewErrInvalidInput(fmt.Sprintf("access in %q mode is not granted", props.Mode))
	case props.TokenID == "":
		return nil, NewErrInvalidInput("a grant names what it granted")
	case props.ExpiresAt.IsZero():
		return nil, NewErrInvalidInput("a grant expires")
	}
	root, err := aggregate.NewRoot(SecretStoreGrant{}.Prefix(), grantID)
	if err != nil {
		return nil, NewErrInvalidInput(err.Error())
	}
	return &SecretStoreGrant{
		Root:           root,
		organizationID: props.OrganizationID,
		storeID:        props.StoreID,
		deploymentID:   props.DeploymentID,
		mode:           props.Mode,
		tokenID:        props.TokenID,
		expiresAt:      props.ExpiresAt.UTC(),
		revokedAt:      clock.CopyTime(props.RevokedAt),
		createdAt:      optional.Or(props.CreatedAt, clock.Now()),
	}, nil
}

func (SecretStoreGrant) Prefix() string { return "ssg" }

func (g *SecretStoreGrant) OrganizationID() string { return g.organizationID }
func (g *SecretStoreGrant) StoreID() string        { return g.storeID }
func (g *SecretStoreGrant) DeploymentID() string   { return g.deploymentID }
func (g *SecretStoreGrant) Mode() AccessMode       { return g.mode }
func (g *SecretStoreGrant) ExpiresAt() time.Time   { return g.expiresAt }

func (g *SecretStoreGrant) Snapshot() SecretStoreGrantSnapshot {
	return SecretStoreGrantSnapshot{
		ID: g.ID(), OrganizationID: g.organizationID, StoreID: g.storeID, DeploymentID: g.deploymentID,
		Mode: g.mode, TokenID: g.tokenID, ExpiresAt: g.expiresAt, RevokedAt: clock.CopyTime(g.revokedAt),
		CreatedAt: g.createdAt,
	}
}
