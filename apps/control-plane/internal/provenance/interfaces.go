package provenance

import (
	"context"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// ListChangesQuery lists the Changes recorded for a Source. It sorts by
// "created_at", with the ID breaking ties.
type ListChangesQuery struct {
	OrganizationID string
	SourceID       string
	Page           pagination.Query
}

// ChangeRepository persists Changes and their commits. Every method is
// scoped to an Organization. It returns ErrNotFound when no row matches, and
// ErrAlreadyExists for a commit another Change in the Source already holds.
type ChangeRepository interface {
	CreateChange(ctx context.Context, c *Change) error
	GetChange(ctx context.Context, organizationID, changeID string) (*Change, error)
	// ChangesForCommits returns the Changes holding any of the commits.
	ChangesForCommits(ctx context.Context, organizationID, sourceID string, commitSHAs []string) ([]*Change, error)
	ListChanges(ctx context.Context, query ListChangesQuery) (*pagination.Result[*Change], error)
}

// RecordChangeInput describes a Change to record.
type RecordChangeInput struct {
	OrganizationID string
	SourceID       string
	SessionID      *string
	WorkItemID     *string
	Title          string
	ExternalRef    *string
	CommitSHAs     []string
}

// Lookup reads what Ivaldi recorded about commits. It is the contract other
// capabilities, such as delivery, depend on. It does not authorize: the
// caller has already authorized its own operation.
type Lookup interface {
	ChangesForCommits(ctx context.Context, organizationID, sourceID string, commitSHAs []string) ([]*Change, error)
}

// ChangeService records and reads Changes.
type ChangeService interface {
	Lookup
	RecordChange(ctx context.Context, actor identity.Actor, in RecordChangeInput) (*Change, error)
	GetChange(ctx context.Context, actor identity.Actor, organizationID, changeID string) (*Change, error)
	ListChanges(ctx context.Context, actor identity.Actor, query ListChangesQuery) (*pagination.Result[*Change], error)
}
