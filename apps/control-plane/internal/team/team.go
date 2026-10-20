// Package team is the control plane's team capability (ADR 0007): Teams of
// humans and Agents that own Projects and Applications.
//
// So far it holds only the contract other capabilities depend on.
package team

import "context"

// Directory answers questions about Teams. Capabilities that record an
// owning Team check it here before storing it.
type Directory interface {
	// IsActiveTeam reports whether teamID is an active Team in the Organization.
	IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error)
}
