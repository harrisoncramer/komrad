// Package xslice holds small generic slice helpers.
package xslice

// Map applies f to every element of in, returning the results in order.
func Map[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}

	return out
}

// Filter returns the elements of in for which keep reports true, preserving order.
func Filter[T any](in []T, keep func(T) bool) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		if keep(v) {
			out = append(out, v)
		}
	}

	return out
}

// Count is how many elements of in satisfy keep, without allocating the slice Filter would.
func Count[T any](in []T, keep func(T) bool) int {
	n := 0
	for _, v := range in {
		if keep(v) {
			n++
		}
	}

	return n
}

// KeyBy indexes in by the key key returns for each element. If two elements share a key, the
// later one wins.
func KeyBy[T any, K comparable](in []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(in))
	for _, v := range in {
		out[key(v)] = v
	}

	return out
}

// GroupBy buckets the elements of in by the key key returns for each one, preserving order
// within a bucket.
func GroupBy[T any, K comparable](in []T, key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for _, v := range in {
		out[key(v)] = append(out[key(v)], v)
	}

	return out
}
