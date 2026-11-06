package authorization

import "context"

// Code that authorizes an operation and code that carries it out are often
// apart: a service is wrapped by one that authorizes each call before
// delegating. The decision travels between them in the context, so the
// operation can stamp its audit record with the decision that allowed it,
// and refuse to run when there is none.

type decisionsKey struct{}

// WithDecision returns ctx carrying d as the decision for subject: the ID of
// the object the audit record will name. An empty subject sets the decision
// for the whole call, such as for creating an object whose ID is not known
// yet; a call that changes several objects sets one per object.
func WithDecision(ctx context.Context, subject string, d Decision) context.Context {
	prev, _ := ctx.Value(decisionsKey{}).(map[string]Decision)
	next := make(map[string]Decision, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	next[subject] = d
	return context.WithValue(ctx, decisionsKey{}, next)
}

// DecisionFor returns the decision ctx carries for subject, or else the one
// it carries for the whole call.
func DecisionFor(ctx context.Context, subject string) (Decision, bool) {
	decisions, _ := ctx.Value(decisionsKey{}).(map[string]Decision)
	if d, ok := decisions[subject]; ok {
		return d, true
	}
	d, ok := decisions[""]
	return d, ok
}
