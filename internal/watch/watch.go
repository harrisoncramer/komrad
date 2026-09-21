// Package watch tells whoever is watching the panel's rows which ones moved.
//
// A store touches the ids it wrote once the write can be read. The broker coalesces the
// touches, reads the rows back, and sends each watcher the row or, for an id the listing no
// longer holds, a tombstone.
package watch
