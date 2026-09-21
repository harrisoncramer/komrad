// Package watch tells whoever is watching the panel's rows which ones moved.
//
// A writer touches the ids it wrote after its commit. The broker coalesces the touches, reads
// the rows back, and sends each watcher the row or, for an id the listing no longer holds, a
// tombstone.
package watch

// Publisher is what a writer tells after its commit.
type Publisher interface {
	TouchAgents(ids ...string)
	TouchWorkflows(ids ...string)
	TouchSchedules(ids ...string)
}

// Discard is the publisher nothing watches.
var Discard Publisher = discard{}

type discard struct{}

func (discard) TouchAgents(...string) {}

func (discard) TouchWorkflows(...string) {}

func (discard) TouchSchedules(...string) {}
