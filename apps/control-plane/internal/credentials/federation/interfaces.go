package federation

// Issuer signs the JWTs grants carry.
type Issuer interface {
	// URL is the issuer: the control plane's external URL and Path.
	URL() string
	Issue(g Grant) (Token, error)
}
