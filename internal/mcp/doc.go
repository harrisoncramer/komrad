// Package mcp serves komrad's API to agents as MCP tools.
//
// The tools come from the same proto descriptors as the handlers, through rpcapi, so a tool takes
// its request's fields as inputs and its annotation says whether it only reads. Each server is
// built for one role, and a writer and a reviewer are each offered the tools their role is
// annotated for. The package also offers read-only SQL over komrad's database.
package mcp
