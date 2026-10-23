package xptr

// Ptr returns a pointer to v, for the fields and arguments that take one.
func Ptr[T any](v T) *T {
	return &v
}

// NonEmpty returns a pointer to v, or nil for the zero value.
func NonEmpty[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}

// Equal reports whether two pointers are both nil or point at equal values.
func Equal[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// Deref returns the value p points at, or the zero value for nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}
