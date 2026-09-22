// Package watch tells whoever is watching the panel's rows which ones moved.
//
// A trigger on each table raises the table and the key of a row that moved, delivered once the
// write has committed. The broker hears them on one connection of its own, coalesces them,
// reads the rows back, and sends each watcher the row or, for an id the listing no longer
// holds, a tombstone.
package watch
