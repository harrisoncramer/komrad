// Command protoc-gen-weave-rpc generates the strict half of every RPC handler: the interface a
// service implements and the adapter that satisfies the one connect generated.
//
// Connect fixes a handler's signature at a plain error, the type that lets a failure reach a
// caller unclassified. The interface written here demands weave's own failure type instead, so
// a handler cannot return an error nobody decided the kind of, and the adapter carries that
// failure across to the signature connect insists on.
package main

import (
	"weave/internal/rpcgen"

	"google.golang.org/protobuf/compiler/protogen"
)

func main() {
	protogen.Options{}.Run(rpcgen.Generate)
}
