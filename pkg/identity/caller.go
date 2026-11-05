package identity

import "context"

// Caller is who made a request, as its verified token says: the Actor, the
// Organization it acts in, and the client and scopes the token was issued
// for. Authentication middleware puts it in the request's context.
type Caller struct {
	Actor          Actor
	OrganizationID string
	ClientID       string
	Scopes         []string
}

// CallerOf returns the Caller a token's claims describe.
func CallerOf(c *Claims) Caller {
	return Caller{Actor: c.Actor(), OrganizationID: c.OrganizationID, ClientID: c.ClientID, Scopes: c.Scopes}
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
