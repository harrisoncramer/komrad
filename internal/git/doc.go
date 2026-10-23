// Package git runs git for every part of komrad that needs it, with one convention for
// timeouts, context and error wrapping.
//
// A [Client] holds the operations komrad uses: reading a checkout's branch, tree state and diff
// stat, rebasing and pushing, taking and landing snapshots, and finding where a branch left its
// base. An [EngineClient] runs the same commands on a remote engine through its API.
package git
