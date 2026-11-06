package workspace

import (
	"context"
	"fmt"
)

// CheckDependents asks every dependent what is still active under target,
// and refuses with a DependentsError listing what is, leaving out kinds with
// nothing active.
func CheckDependents(ctx context.Context, dependents []Dependent, target ArchiveTarget) error {
	var active []Dependency
	for _, d := range dependents {
		deps, err := d.ActiveDependents(ctx, target)
		if err != nil {
			return fmt.Errorf("check dependents: %w", err)
		}
		for _, dep := range deps {
			if dep.Count > 0 {
				active = append(active, dep)
			}
		}
	}
	if len(active) > 0 {
		return &DependentsError{Dependencies: active}
	}
	return nil
}
