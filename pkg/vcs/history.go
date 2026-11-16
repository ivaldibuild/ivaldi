package vcs

import "context"

// History reads commit history for a Source. The control plane's
// version-control capability implements it, resolving the Source's
// Connection and lookup path before calling a Provider.
type History interface {
	// Compare returns what moving the Source from one commit to another
	// adds and removes.
	Compare(ctx context.Context, organizationID, sourceID, from, to string) (Comparison, error)
}
