// Package policy is the control plane's policy capability: the governance
// rules Organizations author about what work may happen (ADR 0003).
//
// So far it holds one kind of rule, setting locks: an Organization can lock
// a setting key for all its Workspaces and Projects, and a Workspace for its
// Projects (docs/design/workspace-data-model.md). The workspace capability
// reads them through workspace.LockSource, which NewLockSource implements,
// to refuse changes below a lock and to resolve locked values. There is no
// evaluation engine and no approval workflow yet; pkg/policy stays empty
// until there is.
//
// This package holds the domain types and the repository and service
// interfaces. The service lives in the locks sub-package, with its HTTP
// API, and the PostgreSQL repository in postgres. Domain objects are the
// only way to change domain state: their fields are unexported, they are
// created with New functions that apply the creation rules, and repositories
// rebuild stored ones with the same functions.
package policy

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Domain types whose IDs carry a prefix register it, so a prefix used twice
// fails when the control plane starts.
func init() {
	id.MustRegister(SettingLock{}.Prefix(), "policy.SettingLock")
	id.MustRegister(ApprovalRequest{}.Prefix(), "policy.ApprovalRequest")
}

// newRoot returns an aggregate's identity: storedID, a stored row's ID given
// to a New constructor, which must have the type's prefix, or a new ID.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, NewErrInvalidInput(err.Error())
	}
	return r, nil
}

func timeOr(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}

func errorf(format string, args ...any) error {
	return NewErrInvalidInput(fmt.Sprintf(format, args...))
}
