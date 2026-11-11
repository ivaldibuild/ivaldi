package iam

import "slices"

// platformPermissions are the permissions each component holds as a platform
// workload, across every Organization (ADR 0015). Platform workloads hold no
// grants, because grants are on an Organization's resources; adding a
// component or a permission here is a deliberate change. The permission
// names belong to the capabilities that ask about them.
var platformPermissions = map[WorkloadComponent][]string{
	// The gateway resolves webhook route keys before it knows which
	// Organization a delivery belongs to.
	ComponentGateway: {"version_control.webhook_route.resolve"},
}

// PlatformComponentAllows reports whether a platform workload of the
// component holds permission.
func PlatformComponentAllows(component WorkloadComponent, permission string) bool {
	return slices.Contains(platformPermissions[component], permission)
}

func isPlatformComponent(component WorkloadComponent) bool {
	_, ok := platformPermissions[component]
	return ok
}
