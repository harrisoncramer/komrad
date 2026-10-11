package ui

import "time"

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

type filterKey struct {
	narrows    bool
	query      string
	compiledAt time.Time
	said       string
	waiting    bool
}

func (f filterState) key() filterKey {
	if !f.doesNarrow(sectionList) {
		return filterKey{}
	}

	return filterKey{
		narrows:    true,
		query:      f.compiledQuery,
		compiledAt: f.qualifiers.compiledAt,
		said:       f.qualifiers.said.text,
		waiting:    f.qualifiers.said.waiting,
	}
}

type listingKey struct {
	generation int
	filter     filterKey
}

func (m *model) listingKey() listingKey {
	return listingKey{generation: m.listing.generation, filter: m.filter.key()}
}
