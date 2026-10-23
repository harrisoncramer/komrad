// Package watch streams changes from the database to each watching panel.
//
// Database triggers notify a Postgres channel when a watched row changes. The [Broker] listens on
// that channel with a connection of its own and gathers the keys it hears about. It reads those
// rows back through the same store and render code the list RPCs use, and sends each
// [Subscription] what changed, or a tombstone for a row it should drop. It compares each row with
// the last one it sent for that key, so a write that moves only a timestamp sends nothing.
//
// A subscription opens with a snapshot of every row. The broker drops every subscription when its
// connection is lost, and the panel takes a fresh snapshot instead of replaying missed events.
// [Resource] declares each watched table and the tables that feed it, and the triggers package
// builds the triggers from those declarations.
package watch
