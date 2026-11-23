package federation

import "time"

// Grant is what one JWT is for: one store, read for one Deployment.
type Grant struct {
	OrganizationID  string
	WorkspaceID     string
	ProjectID       string
	ApplicationID   string
	EnvironmentID   string
	EnvironmentTier string
	TargetID        string
	ClusterID       string
	DeploymentID    string
	ReleaseID       string
	StoreID         string
}

// Token is a signed JWT. Its value is a credential: it is sent to the agent
// and never stored, logged, or put in a record.
type Token struct {
	Value     string
	ID        string
	ExpiresAt time.Time
}

// Claims are a JWT's claims: the registered ones, and the IDs a store with
// bound claims can match on.
type Claims struct {
	Issuer          string   `json:"iss"`
	Subject         string   `json:"sub"`
	Audience        []string `json:"aud"`
	IssuedAt        int64    `json:"iat"`
	NotBefore       int64    `json:"nbf"`
	ExpiresAt       int64    `json:"exp"`
	ID              string   `json:"jti"`
	Organization    string   `json:"org"`
	Workspace       string   `json:"workspace"`
	Project         string   `json:"project"`
	Application     string   `json:"application"`
	Environment     string   `json:"environment"`
	EnvironmentTier string   `json:"environment_tier"`
	Target          string   `json:"target"`
	Cluster         string   `json:"cluster"`
	Deployment      string   `json:"deployment"`
	Release         string   `json:"release"`
}

// discovery is the issuer's OpenID Provider Metadata: only what a relying
// party that verifies JWTs needs. The issuer signs JWTs for grants and runs
// no OAuth flow, so it has no authorization or token endpoint.
type discovery struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	ClaimsSupported                  []string `json:"claims_supported"`
}
