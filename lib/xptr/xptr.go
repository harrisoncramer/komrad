// Package xptr holds small generic pointer helpers.
package xptr

// Ptr returns a pointer to v, for the fields and arguments that take one.
func Ptr[T any](v T) *T {
	return &v
}
