package watch

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"komrade/internal/logging"
	watchv1 "komrade/proto/gen/watch/v1"
)

// pendingKeys is the keys one render reads back, by resource, each resource's in order.
type pendingKeys map[watchv1.Resource][]string

// signal says the rows of a resource with these keys may have moved.
func (b *Broker) signal(resource watchv1.Resource, keys ...string) {
	if !b.rows.hasLoader(resource) {
		if b.shouldWarn("resource " + resource.String()) {
			b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a signal named a resource nothing reads", slog.String(logging.WatchResource, resource.String()))
		}

		return
	}

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

// shouldWarn reports whether a key is new to the broker, remembering it either way.
func (b *Broker) shouldWarn(key string) bool {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	_, said := b.unknown[key]
	b.unknown[key] = struct{}{}

	return !said
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
		b.signal(res, keys...)
	}
}
