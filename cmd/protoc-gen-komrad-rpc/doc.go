// Command protoc-gen-komrad-rpc is the protoc plugin that generates komrad's service code from
// the protos through rpcgen. For each service it writes a strict handler interface, an adapter
// onto Connect, and the parsers that convert each request.
//
// Connect fixes a handler's error type at a plain error. The generated interface returns komrad's
// own failure type instead, so every failure a handler returns has a kind, and the adapter turns
// it into the error Connect expects.
package main
