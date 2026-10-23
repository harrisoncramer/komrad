// Package git runs git for every part of komrad that needs it, with one convention for
// timeouts, context and error wrapping.
//
// A [Client] holds the operations komrad uses. It reads a checkout's branch, tree state and diff
// stat, rebases and pushes, takes and lands snapshots, and finds where a branch left its base
// branch. An [EngineClient] runs the same commands on a remote engine through its API.
package git
