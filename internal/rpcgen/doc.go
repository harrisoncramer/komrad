// Package rpcgen generates komrad's service code from the protos. For each service it writes a
// strict handler interface and an adapter onto Connect, so a handler returns a typed failure and
// rpcerr picks the code for it.
//
// A unary method answers with a message and a failure, and a streaming method answers with a
// failure alone. The generator refuses a method that streams only from the client.
//
// A request field whose validation rules describe a Go type of its own reaches the handler parsed.
// A request holding a value that would not convert is refused as an invalid argument before the
// handler sees it. A oneof holding no message reaches the handler as a type in the oneofs package,
// one type for every oneof with that name.
package rpcgen
