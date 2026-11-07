// Package identity describes who is acting in Ivaldi. Any deployable that
// handles a request, such as the control plane, the gateway, or a worker,
// passes an Actor to the single authorizer (ADR 0003). Offline token
// verification, also described in ADR 0003, belongs here as well.
package identity

// ActorKind is the kind of principal acting.
type ActorKind string

const (
	ActorUser           ActorKind = "user"
	ActorServiceAccount ActorKind = "service_account"
	ActorAgentSession   ActorKind = "agent_session"
	// ActorWorkload is an enrolled data-plane component, such as a runtime agent.
	ActorWorkload ActorKind = "workload"
	// ActorSystem is Ivaldi itself, such as when provisioning a new Organization.
	ActorSystem ActorKind = "system"
)

// Actor is who is asking for an action.
type Actor struct {
	PrincipalID string
	Kind        ActorKind
	// SessionID is set when the actor is an agent Session.
	SessionID string
}

// IsAgent reports whether the actor is an agent Session.
func (a Actor) IsAgent() bool { return a.Kind == ActorAgentSession }

func (a Actor) IsSystem() bool { return a.Kind == ActorSystem }

// IsUser reports whether the actor is a user.
func (a Actor) IsUser() bool { return a.Kind == ActorUser }

// IsServiceAccount reports whether the actor is a service account.
func (a Actor) IsServiceAccount() bool { return a.Kind == ActorServiceAccount }

// IsWorkload reports whether the actor is an enrolled data-plane workload.
func (a Actor) IsWorkload() bool { return a.Kind == ActorWorkload }
