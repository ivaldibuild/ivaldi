package identity

import (
	"context"
	"time"
)

// Caller is who made a request, as its verified token says: the Actor, the
// Organization it acts in, the client and scopes the token was issued for,
// and, for a workload credential, the credential and its expiry.
// Authentication middleware puts it in the request's context.
type Caller struct {
	Actor          Actor
	OrganizationID string
	ClientID       string
	Scopes         []string
	// CredentialID names the credential that authenticated a workload.
	CredentialID string
	// CredentialExpiresAt is when that credential stops being valid.
	CredentialExpiresAt time.Time
}

// CallerOf returns the Caller a token's claims describe. Only a workload's
// caller carries a workload credential ID and expiry.
func CallerOf(c *Claims) Caller {
	caller := Caller{
		Actor: c.Actor(), OrganizationID: c.OrganizationID, ClientID: c.ClientID, Scopes: c.Scopes,
	}
	if caller.Actor.Kind == ActorWorkload {
		caller.CredentialID = c.ID
		caller.CredentialExpiresAt = c.ExpiresAt
	}
	return caller
}

type callerKey struct{}

// WithCaller returns ctx carrying the caller.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the caller ctx carries, if a request was authenticated.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}
