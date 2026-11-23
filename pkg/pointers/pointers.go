// Package pointers copies and compares the values pointers point to.
package pointers

// Copy returns a pointer to a copy of *p, or nil when p is nil, so a value
// can be handed out without sharing memory. Unlike optional.OfPtr, it keeps
// a zero value. The copy is shallow.
func Copy[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// Equal reports whether a and b are both nil, or both point to equal values.
func Equal[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
