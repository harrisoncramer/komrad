// Package protoconv converts the protobuf values that protovalidate rules describe into the Go
// types they stand for, such as a string field holding a UUID, accepting exactly what each rule
// accepts. The request parsers protoc-gen-komrad-rpc generates call it.
package protoconv
