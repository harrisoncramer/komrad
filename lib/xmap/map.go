// Package xmap holds small generic map helpers.
package xmap

// Set writes a value into a map held behind a pointer, making the map on the first write and
// replacing whatever the key held.
func Set[K comparable, V any](into *map[K]V, key K, value V) {
	if *into == nil {
		*into = map[K]V{}
	}

	(*into)[key] = value
}
