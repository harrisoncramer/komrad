package main

import (
	"komrad/internal/rpcgen"

	"google.golang.org/protobuf/compiler/protogen"
)

func main() {
	protogen.Options{}.Run(rpcgen.Generate)
}
