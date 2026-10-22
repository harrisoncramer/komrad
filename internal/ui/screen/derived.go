package screen

import "komrad/internal/ui/list"

type Derived[K comparable, V any] struct {
	built bool
	key   K
	value V
}

// Read returns the value built for key, building it again when the key has changed.
func (d *Derived[K, V]) Read(key K, build func() V) V {
	if !d.built || d.key != key {
		*d = Derived[K, V]{
			built: true,
			key:   key,
			value: build(),
		}
	}

	return d.value
}

type ListingKey struct {
	generation int
	filter     list.PatternKey
}

// ListingKey is the key of the listing and the filter these inputs carry.
func (in Inputs) ListingKey() ListingKey {
	return ListingKey{generation: in.Listing.Generation, filter: in.Room.Pattern.Key()}
}
