// Package xptr holds small generic pointer helpers.
package xptr

// Ptr returns a pointer to v, for the fields and arguments that take one.
func Ptr[T any](v T) *T {
	return &v
}

// NonEmpty returns a pointer to s, or nil for an empty string.
func NonEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
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
