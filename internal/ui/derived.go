package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
)

type derived[K comparable, V any] struct {
	built bool
	key   K
	value V
}

func (d *derived[K, V]) read(key K, build func() V) V {
	if !d.built || d.key != key {
		*d = derived[K, V]{
			built: true,
			key:   key,
			value: build(),
		}
	}

	return d.value
}

type listingKey struct {
	generation int
	filter     list.PatternKey
}

func listingKeyOf(in screen.Inputs) listingKey {
	return listingKey{generation: in.Listing.Generation, filter: in.Room.Pattern.Key()}
}
