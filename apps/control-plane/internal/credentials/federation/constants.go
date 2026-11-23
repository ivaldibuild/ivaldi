package federation

import "time"

const (
	// Path is the issuer's path under the control plane's external URL.
	Path = "/federation"
	// DiscoveryPath is where OIDC Discovery looks for an issuer with a path:
	// the issuer, then /.well-known/openid-configuration.
	DiscoveryPath = Path + "/.well-known/openid-configuration"
	// JWKSPath publishes the keys that verify the JWTs.
	JWKSPath = Path + "/jwks.json"
	// Lifetime is how long a JWT may be exchanged: long enough for an agent
	// to reach its store, short enough that one that leaks is soon useless.
	Lifetime = 5 * time.Minute
	// tokenType is the JWTs' typ header.
	tokenType = "JWT"
)
