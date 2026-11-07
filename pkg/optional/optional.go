// Package optional converts values to the pointers that optional fields,
// nullable columns, and generated API clients use for "not set".
package optional

// Of returns a pointer to v, or nil when v is its type's zero value, such as
// an empty string. Use it only where the zero value means "not set"; for a
// pointer to any value, use new(v).
func Of[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// OfPtr is Of for a value that may itself be missing: it returns nil when p
// is nil or points to the zero value, and otherwise a pointer to a copy of
// *p, so the result never shares memory with p.
func OfPtr[T comparable](p *T) *T {
	if p == nil {
		return nil
	}
	return Of(*p)
}
