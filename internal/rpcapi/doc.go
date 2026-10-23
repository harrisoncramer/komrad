// Package rpcapi reads komrad's own proto descriptors at runtime. The CLI, the MCP server and the
// docs generator build their commands and tools from the methods it lists, with each method's
// request fields, validation rules and annotations. A new RPC appears as a CLI command and an MCP
// tool without code written for either.
//
// The package also holds the HTTP transport komrad's clients call the server over, which waits on
// a call that awaits hooks without a header timeout.
package rpcapi
