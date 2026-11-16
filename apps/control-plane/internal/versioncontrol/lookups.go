package versioncontrol

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Lookups reads commit history for a Source. It implements vcs.History, so
// capabilities such as delivery ask for commits without reading this
// capability's tables.
//
// Lookups does not authorize: the calling capability has already authorized
// its own operation. It does enforce the Connection's lookup path, so a
// Connection that runs lookups in the data plane is never contacted from the
// control plane (ADR 0008).
type Lookups struct {
	sources     SourceRepository
	connections ConnectionRepository
	provider    vcs.Provider
}

// NewLookups returns Lookups, or an error naming a missing repository. A nil
// provider is allowed: the control plane starts without a configured
// provider, and a lookup then reports that none is supported.
func NewLookups(sources SourceRepository, connections ConnectionRepository, provider vcs.Provider) (*Lookups, error) {
	switch {
	case sources == nil:
		return nil, errors.New("version-control lookups: a SourceRepository is required")
	case connections == nil:
		return nil, errors.New("version-control lookups: a ConnectionRepository is required")
	}
	return &Lookups{sources: sources, connections: connections, provider: provider}, nil
}

var _ vcs.History = (*Lookups)(nil)

// Compare returns what moving the Source from one commit to another adds
// and removes.
func (l *Lookups) Compare(ctx context.Context, organizationID, sourceID, from, to string) (vcs.Comparison, error) {
	if from == "" || to == "" {
		return vcs.Comparison{}, vcs.ErrInvalidRange
	}
	source, err := l.sources.GetSource(ctx, organizationID, sourceID)
	if err != nil {
		return vcs.Comparison{}, translateLookupError(err)
	}
	if source.Archived() {
		return vcs.Comparison{}, vcs.ErrSourceArchived
	}
	connection, err := l.connections.GetConnection(ctx, organizationID, source.ConnectionID())
	if err != nil {
		return vcs.Comparison{}, translateLookupError(err)
	}
	if !connection.Usable() {
		return vcs.Comparison{}, vcs.ErrConnectionInactive
	}
	if connection.LookupFrom() == vcs.LookupFromDataPlane {
		return vcs.Comparison{}, vcs.ErrLookupInDataPlane
	}
	if l.provider == nil {
		return vcs.Comparison{}, vcs.ErrUnsupportedProvider
	}
	if connection.AuthKind() != AuthKindAppInstallation {
		return vcs.Comparison{}, vcs.ErrUnsupportedProvider
	}
	return l.provider.Compare(ctx, connection.Endpoint(), source.repository(connection), from, to)
}

// repository returns the provider's view of the stored Source.
func (s *Source) repository(connection *Connection) vcs.Repository {
	return vcs.Repository{
		Provider:      connection.Provider(),
		HostURL:       connection.HostURL(),
		ExternalID:    s.externalRepositoryID,
		FullName:      s.fullName,
		DefaultBranch: s.defaultBranch,
		Visibility:    string(s.visibility),
		CloneURL:      deref(s.cloneURL),
		WebURL:        deref(s.webURL),
	}
}

// translateLookupError keeps a lookup's errors in the vcs package's
// vocabulary, so callers react to one set of errors.
func translateLookupError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: %w", vcs.ErrNotFound, err)
	}
	return err
}
