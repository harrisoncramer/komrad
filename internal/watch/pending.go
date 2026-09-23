package watch

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"weave/internal/logging"
	watchv1 "weave/proto/gen/watch/v1"
)

// pendingKeys is the keys one render reads back, by resource, each resource's in order.
type pendingKeys map[watchv1.Resource][]string

// Signal says the rows of a resource with these keys may have moved.
func (b *Broker) Signal(resource watchv1.Resource, keys ...string) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	if b.pending == nil {
		return
	}

	pending := b.pending[resource]
	if pending == nil {
		pending = map[string]struct{}{}
		b.pending[resource] = pending
	}
	for _, key := range keys {
		pending[key] = struct{}{}
	}

	// Poke the loop unless it has an unread poke.
	select {
	case b.wake <- struct{}{}:
	default:
	}
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

// drainPending empties the pending set and returns it with each resource's keys in order.
func (b *Broker) drainPending() pendingKeys {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	pending := pendingKeys{}
	for res, keys := range b.pending {
		pending[res] = slices.Sorted(maps.Keys(keys))
	}
	clear(b.pending)

	return pending
}

// requeuePending puts the keys a failed render drained back for the next one.
func (b *Broker) requeuePending(pending pendingKeys) {
	for res, keys := range pending {
		b.Signal(res, keys...)
	}
}
