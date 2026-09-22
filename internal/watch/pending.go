package watch

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"weave/internal/logging"
	watchv1 "weave/proto/gen/watch/v1"
)

// touchedKeys is the keys one render reads back, by resource, each resource's in order.
type touchedKeys map[watchv1.Resource][]string

// touch says the row of a table with this key may have moved.
func (b *Broker) touch(table, key string) {
	res, ok := resourcesByTable[table]
	if !ok {
		b.reportUnknown(table)

		return
	}

	b.touchResource(res, []string{key})
}

// reportUnknown says once that a table nothing reads is still announcing itself.
func (b *Broker) reportUnknown(table string) {
	b.pendingMu.Lock()
	_, said := b.unknown[table]
	if !said {
		b.unknown[table] = struct{}{}
	}
	b.pendingMu.Unlock()

	if said {
		return
	}

	b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a notification named a table no resource is a row of", slog.String(logging.WatchTable, table))
}

func (b *Broker) touchResource(res watchv1.Resource, keys []string) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	if b.pending == nil {
		return
	}

	touched := b.pending[res]
	if touched == nil {
		touched = map[string]struct{}{}
		b.pending[res] = touched
	}
	for _, key := range keys {
		touched[key] = struct{}{}
	}

	// Poke the loop unless it has an unread poke.
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// take empties the pending set and returns it with each resource's keys in order.
func (b *Broker) take() touchedKeys {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	touched := touchedKeys{}
	for res, keys := range b.pending {
		touched[res] = slices.Sorted(maps.Keys(keys))
	}
	clear(b.pending)

	return touched
}

// restore puts the keys a failed render took back for the next one.
func (b *Broker) restore(touched touchedKeys) {
	for res, keys := range touched {
		b.touchResource(res, keys)
	}
}
