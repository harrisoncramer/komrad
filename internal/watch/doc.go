// Package watch is the coordinator between the panel's rows and the database.
//
// The package watch handles the collection of data via the Postgres LISTEN notifications.
// The broker gathers those hints, reads the named rows back through the same store and render code the list RPCs use, and sends
// each panel whatever differs. If a hint is lost, the panel takes a fresh snapshot.
//
// The Broker holds one connection of its own on the watch channel, coalesces what it receives into
// a pending set of IDs, then reads those rows and sends each Subscription the row (or for a key its
// watched set no longer holds, a tombstone).
//
// Every Subscription is dropped when that connection is lost. This is intentional. Weave doesn't
// replay events or store a durable event log. Instead, the pane just re-snapshots the database, which
// is enough for a UI with a few thousand records at most.
//
// The database triggers fire on any change outside a resource's ignored columns; it's the broker's
// responsibility to compare each row it reads back against the last one it sent for that key, so a
// write that moves only a timestamp, for instance, doesn't send along the new data.
package watch
