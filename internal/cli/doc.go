// Package cli builds the komrad command tree. Its commands serve the engine, open the panel,
// attach to an agent, run the doctor, connect a forge, sign in to a remote engine, and carry the
// hooks each harness runs.
//
// The rpc subcommands come from the same proto descriptors as the handlers and the MCP tools,
// through rpcapi, so every RPC can be called from a shell without a command written for it.
package cli
