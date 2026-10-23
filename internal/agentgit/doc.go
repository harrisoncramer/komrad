// Package agentgit runs git on the engine for a panel on another machine, and only in the
// directories live agents work in. The server uses it to answer the git RPCs, which the panel
// calls to read a snapshot's patch when the engine is remote.
package agentgit
