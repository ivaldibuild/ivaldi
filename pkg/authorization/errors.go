package authorization

import (
	"errors"
	"fmt"
)

var (
	// ErrDenied means the authorizer refused the action.
	ErrDenied = errors.New("denied")
	// ErrApprovalRequired means the action waits for a human's approval.
	ErrApprovalRequired = errors.New("approval required")
)

// DeniedError reports a refusal by the authorizer. It matches ErrDenied.
type DeniedError struct {
	DecisionID string
	Reason     string
}

func (e *DeniedError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("denied (decision %s)", e.DecisionID)
	}
	return fmt.Sprintf("denied: %s (decision %s)", e.Reason, e.DecisionID)
}

func (e *DeniedError) Is(target error) bool { return target == ErrDenied }

// ApprovalRequiredError reports that the action waits for a human's approval.
// The policy capability holds the request under DecisionID. Resubmitting the
// action with that ID as the Request's ApprovalID applies it once approved.
// It matches ErrApprovalRequired.
type ApprovalRequiredError struct {
	DecisionID string
}

func (e *ApprovalRequiredError) Error() string {
	return fmt.Sprintf("approval required (decision %s)", e.DecisionID)
}

func (e *ApprovalRequiredError) Is(target error) bool { return target == ErrApprovalRequired }
