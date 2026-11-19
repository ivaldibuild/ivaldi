package approvals

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
)

// lookup reads approval requests for other capabilities, without
// authorizing.
type lookup struct {
	options
}

var _ policy.ApprovalLookup = (*lookup)(nil)

// NewLookup returns the policy.ApprovalLookup other capabilities read
// requests through, or an error naming a missing dependency.
func NewLookup(opts ...Option) (policy.ApprovalLookup, error) {
	o := newOptions(opts)
	if err := requireAll("approval lookup",
		dependency{"ApprovalRequestRepo", o.requests != nil},
		dependency{"HeldResources", o.held != nil},
	); err != nil {
		return nil, err
	}
	return &lookup{options: o}, nil
}

func (l *lookup) LookupApprovalRequest(ctx context.Context, organizationID, requestID string) (*policy.ApprovalRequest, error) {
	return l.requests.GetApprovalRequest(ctx, organizationID, requestID)
}

func (l *lookup) Deciders(ctx context.Context, r *policy.ApprovalRequest, userIDs []string) ([]string, error) {
	var out []string
	for _, id := range userIDs {
		could, err := l.userCouldDecide(ctx, id, r)
		if err != nil {
			return nil, err
		}
		if could {
			out = append(out, id)
		}
	}
	return out, nil
}
