// Package cli builds the komrad command tree: serving the engine, opening the panel, attaching
// to an agent, running the doctor, connecting a forge, signing in to a remote engine, and the
// hooks each harness runs.
//
// The rpc subcommands come from the same proto descriptors as the handlers and the MCP tools,
// through rpcapi, so every RPC can be called from a shell without a command written for it.
package cli
