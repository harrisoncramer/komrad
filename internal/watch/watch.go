// Package watch tells whoever is watching the panel's rows which ones moved.
//
// The schema's trigger functions, watch_notify on each resource table and watch_project and
// watch_project_lookup on the tables projected onto one, raise the table and the key of a row
// that moved, delivered once the write has committed. The Broker's listener hears them on one
// connection of its own, coalesces them, reads the rows back through its Reader, and sends each
// Subscription the row or, for a key its watched set no longer holds, a tombstone. A
// Subscription opens with a snapshot of every row, and every Subscription is dropped when the
// listener's connection is lost, so a watcher's picture is never older than the last change it
// could have heard.
package watch
